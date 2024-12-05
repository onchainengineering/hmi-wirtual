package apptest

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/agent"
	agentproto "github.com/onchainengineering/hmi-wirtual/agent/proto"
	"github.com/onchainengineering/hmi-wirtual/cryptorand"
	"github.com/onchainengineering/hmi-wirtual/provisioner/echo"
	"github.com/onchainengineering/hmi-wirtual/provisionersdk/proto"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/workspaceapps"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/workspaceapps/appurl"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk/agentsdk"
)

const (
	proxyTestAgentName            = "agent-name"
	proxyTestAppNameFake          = "test-app-fake"
	proxyTestAppNameOwner         = "test-app-owner"
	proxyTestAppNameAuthenticated = "test-app-authenticated"
	proxyTestAppNamePublic        = "test-app-public"
	proxyTestAppQuery             = "query=true"
	proxyTestAppBody              = "hello world from apps test"

	proxyTestSubdomainRaw = "*.test.coder.com"
	proxyTestSubdomain    = "test.coder.com"
)

// DeploymentOptions are the options for creating a *Deployment with a
// DeploymentFactory.
type DeploymentOptions struct {
	PrimaryAppHost                       string
	AppHost                              string
	DisablePathApps                      bool
	DisableSubdomainApps                 bool
	DangerousAllowPathAppSharing         bool
	DangerousAllowPathAppSiteOwnerAccess bool
	ServeHTTPS                           bool

	StatsCollectorOptions workspaceapps.StatsCollectorOptions

	// The following fields are only used by setupProxyTestWithFactory.
	noWorkspace bool
	port        uint16
	headers     http.Header
}

// Deployment is a license-agnostic deployment with all the fields that apps
// tests need.
type Deployment struct {
	Options *DeploymentOptions

	// SDKClient should be logged in as the admin user.
	SDKClient      *wirtualsdk.Client
	FirstUser      wirtualsdk.CreateFirstUserResponse
	PathAppBaseURL *url.URL
	FlushStats     func()
}

// DeploymentFactory generates a deployment with an API client, a path base URL,
// and a subdomain app host URL.
type DeploymentFactory func(t *testing.T, opts *DeploymentOptions) *Deployment

// App is similar to httpapi.ApplicationURL but with a Query field.
type App struct {
	Username      string
	WorkspaceName string
	// AgentName is optional, except for when proxying to a port. AgentName is
	// always ignored when making a path app URL.
	//
	// Set WorkspaceName to `workspace.agent` if you want to generate a path app
	// URL with an agent name.
	AgentName     string
	AppSlugOrPort string

	// Prefix should have ---.
	Prefix string
	Query  string
}

// Details are the full test details returned from setupProxyTestWithFactory.
type Details struct {
	*Deployment

	Me wirtualsdk.User

	// The following fields are not set if setupProxyTest was called with
	// `withWorkspace` set to `false`.

	Workspace *wirtualsdk.Workspace
	Agent     *wirtualsdk.WorkspaceAgent
	AppPort   uint16

	Apps struct {
		Fake          App
		Owner         App
		Authenticated App
		Public        App
		Port          App
		PortHTTPS     App
	}
}

// AppClient returns a *wirtualsdk.Client that will route all requests to the
// app server. API requests will fail with this client. Any redirect responses
// are not followed by default.
//
// The client is authenticated as the first user by default.
func (d *Details) AppClient(t *testing.T) *wirtualsdk.Client {
	client := wirtualsdk.New(d.PathAppBaseURL)
	client.SetSessionToken(d.SDKClient.SessionToken())
	forceURLTransport(t, client)
	client.HTTPClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return client
}

// PathAppURL returns the URL for the given path app.
func (d *Details) PathAppURL(app App) *url.URL {
	appPath := fmt.Sprintf("/@%s/%s/apps/%s", app.Username, app.WorkspaceName, app.AppSlugOrPort)

	u := *d.PathAppBaseURL
	u.Path = path.Join(u.Path, appPath)
	u.Path += "/"
	u.RawQuery = app.Query
	return &u
}

// SubdomainAppURL returns the URL for the given subdomain app.
func (d *Details) SubdomainAppURL(app App) *url.URL {
	appHost := appurl.ApplicationURL{
		Prefix:        app.Prefix,
		AppSlugOrPort: app.AppSlugOrPort,
		AgentName:     app.AgentName,
		WorkspaceName: app.WorkspaceName,
		Username:      app.Username,
	}
	u := *d.PathAppBaseURL
	u.Host = strings.Replace(d.Options.AppHost, "*", appHost.String(), 1)
	u.Path = "/"
	u.RawQuery = app.Query
	return &u
}

// setupProxyTestWithFactory does the following:
// 1. Create a deployment with the factory.
// 2. Start a test app server.
// 3. Create a template version, template and workspace with many apps.
// 4. Start a workspace agent.
// 5. Returns details about the deployment and its apps.
func setupProxyTestWithFactory(t *testing.T, factory DeploymentFactory, opts *DeploymentOptions) *Details {
	if opts == nil {
		opts = &DeploymentOptions{}
	}
	if opts.AppHost == "" {
		opts.AppHost = proxyTestSubdomainRaw
	}
	if opts.DisableSubdomainApps {
		opts.AppHost = ""
	}

	deployment := factory(t, opts)

	// Configure the HTTP client to not follow redirects and to route all
	// requests regardless of hostname to the wirtuald test server.
	deployment.SDKClient.HTTPClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	forceURLTransport(t, deployment.SDKClient)

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitMedium)
	defer cancel()

	me, err := deployment.SDKClient.User(ctx, wirtualsdk.Me)
	require.NoError(t, err)

	if opts.noWorkspace {
		return &Details{
			Deployment: deployment,
			Me:         me,
		}
	}

	if opts.port == 0 {
		opts.port = appServer(t, opts.headers, opts.ServeHTTPS)
	}
	workspace, agnt := createWorkspaceWithApps(t, deployment.SDKClient, deployment.FirstUser.OrganizationID, me, opts.port, opts.ServeHTTPS)

	details := &Details{
		Deployment: deployment,
		Me:         me,
		Workspace:  &workspace,
		Agent:      &agnt,
		AppPort:    opts.port,
	}

	details.Apps.Fake = App{
		Username:      me.Username,
		WorkspaceName: workspace.Name,
		AgentName:     agnt.Name,
		AppSlugOrPort: proxyTestAppNameFake,
	}
	details.Apps.Owner = App{
		Username:      me.Username,
		WorkspaceName: workspace.Name,
		AgentName:     agnt.Name,
		AppSlugOrPort: proxyTestAppNameOwner,
		Query:         proxyTestAppQuery,
	}
	details.Apps.Authenticated = App{
		Username:      me.Username,
		WorkspaceName: workspace.Name,
		AgentName:     agnt.Name,
		AppSlugOrPort: proxyTestAppNameAuthenticated,
		Query:         proxyTestAppQuery,
	}
	details.Apps.Public = App{
		Username:      me.Username,
		WorkspaceName: workspace.Name,
		AgentName:     agnt.Name,
		AppSlugOrPort: proxyTestAppNamePublic,
		Query:         proxyTestAppQuery,
	}
	details.Apps.Port = App{
		Username:      me.Username,
		WorkspaceName: workspace.Name,
		AgentName:     agnt.Name,
		AppSlugOrPort: strconv.Itoa(int(opts.port)),
	}
	details.Apps.PortHTTPS = App{
		Username:      me.Username,
		WorkspaceName: workspace.Name,
		AgentName:     agnt.Name,
		AppSlugOrPort: strconv.Itoa(int(opts.port)) + "s",
	}

	return details
}

//nolint:revive
func appServer(t *testing.T, headers http.Header, isHTTPS bool) uint16 {
	server := httptest.NewUnstartedServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				_, err := r.Cookie(wirtualsdk.SessionTokenCookie)
				assert.ErrorIs(t, err, http.ErrNoCookie)
				w.Header().Set("X-Forwarded-For", r.Header.Get("X-Forwarded-For"))
				w.Header().Set("X-Got-Host", r.Host)
				for name, values := range headers {
					for _, value := range values {
						w.Header().Add(name, value)
					}
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(proxyTestAppBody))
			},
		),
	)

	server.Config.ReadHeaderTimeout = time.Minute
	if isHTTPS {
		server.StartTLS()
	} else {
		server.Start()
	}
	t.Cleanup(func() {
		server.Close()
	})

	_, portStr, err := net.SplitHostPort(server.Listener.Addr().String())
	require.NoError(t, err)
	port, err := strconv.ParseUint(portStr, 10, 16)
	require.NoError(t, err)

	return uint16(port)
}

//nolint:revive
func createWorkspaceWithApps(t *testing.T, client *wirtualsdk.Client, orgID uuid.UUID, me wirtualsdk.User, port uint16, serveHTTPS bool, workspaceMutators ...func(*wirtualsdk.CreateWorkspaceRequest)) (wirtualsdk.Workspace, wirtualsdk.WorkspaceAgent) {
	authToken := uuid.NewString()

	scheme := "http"
	if serveHTTPS {
		scheme = "https"
	}

	// Workspace name needs to be short to avoid hitting 62 char hostname
	// segment limit.
	workspaceName, err := cryptorand.String(6)
	require.NoError(t, err)
	workspaceName = "ws-" + workspaceName
	workspaceMutators = append([]func(*wirtualsdk.CreateWorkspaceRequest){
		func(req *wirtualsdk.CreateWorkspaceRequest) {
			req.Name = workspaceName
		},
	}, workspaceMutators...)

	// Intentionally going to choose a port that will never be chosen.
	// Ports <1k will never be selected. 396 is for some old OS over IP.
	// It will never likely be provisioned. Using quick timeout since
	// it's all localhost
	fakeAppURL := "http://127.1.0.1:396"
	conn, err := net.DialTimeout("tcp", fakeAppURL, time.Millisecond*100)
	if err == nil {
		// In the absolute rare case someone hits this. Writing code to find a free port
		// seems like a waste of time to program and run.
		_ = conn.Close()
		t.Errorf("an unused port is required for the fake app. "+
			"The url %q happens to be an active port. If you hit this, then this test"+
			"will need to be modified to run on your system. Or you can stop serving an"+
			"app on that port.", fakeAppURL)
		t.FailNow()
	}

	appURL := fmt.Sprintf("%s://127.0.0.1:%d?%s", scheme, port, proxyTestAppQuery)
	protoApps := []*proto.App{
		{
			Slug:         proxyTestAppNameFake,
			DisplayName:  proxyTestAppNameFake,
			SharingLevel: proto.AppSharingLevel_OWNER,
			Url:          fakeAppURL,
			Subdomain:    true,
		},
		{
			Slug:         proxyTestAppNameOwner,
			DisplayName:  proxyTestAppNameOwner,
			SharingLevel: proto.AppSharingLevel_OWNER,
			Url:          appURL,
			Subdomain:    true,
		},
		{
			Slug:         proxyTestAppNameAuthenticated,
			DisplayName:  proxyTestAppNameAuthenticated,
			SharingLevel: proto.AppSharingLevel_AUTHENTICATED,
			Url:          appURL,
			Subdomain:    true,
		},
		{
			Slug:         proxyTestAppNamePublic,
			DisplayName:  proxyTestAppNamePublic,
			SharingLevel: proto.AppSharingLevel_PUBLIC,
			Url:          appURL,
			Subdomain:    true,
		},
	}
	version := wirtualdtest.CreateTemplateVersion(t, client, orgID, &echo.Responses{
		Parse:         echo.ParseComplete,
		ProvisionPlan: echo.PlanComplete,
		ProvisionApply: []*proto.Response{{
			Type: &proto.Response_Apply{
				Apply: &proto.ApplyComplete{
					Resources: []*proto.Resource{{
						Name: "example",
						Type: "aws_instance",
						Agents: []*proto.Agent{{
							Id:   uuid.NewString(),
							Name: proxyTestAgentName,
							Auth: &proto.Agent_Token{
								Token: authToken,
							},
							Apps: protoApps,
						}},
					}},
				},
			},
		}},
	})
	template := wirtualdtest.CreateTemplate(t, client, orgID, version.ID)
	wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
	workspace := wirtualdtest.CreateWorkspace(t, client, template.ID, workspaceMutators...)
	workspaceBuild := wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)

	// Verify app subdomains
	for _, app := range workspaceBuild.Resources[0].Agents[0].Apps {
		require.True(t, app.Subdomain)

		appURL := appurl.ApplicationURL{
			Prefix: "",
			// findProtoApp is needed as the order of apps returned from PG database
			// is not guaranteed.
			AppSlugOrPort: findProtoApp(t, protoApps, app.Slug).Slug,
			AgentName:     proxyTestAgentName,
			WorkspaceName: workspace.Name,
			Username:      me.Username,
		}
		require.Equal(t, appURL.String(), app.SubdomainName)
	}

	agentClient := agentsdk.New(client.URL)
	agentClient.SetSessionToken(authToken)

	// TODO (@dean): currently, the primary app host is used when generating
	// the port URL we tell the agent to use. We don't have any plans to change
	// that until we let templates pick which proxy they want to use in the
	// terraform.
	//
	// This means that all port URLs generated in code-server etc. will be sent
	// to the primary.
	appHostCtx := testutil.Context(t, testutil.WaitLong)
	primaryAppHost, err := client.AppHost(appHostCtx)
	require.NoError(t, err)
	if primaryAppHost.Host != "" {
		rpcConn, err := agentClient.ConnectRPC(appHostCtx)
		require.NoError(t, err)
		aAPI := agentproto.NewDRPCAgentClient(rpcConn)
		manifest, err := aAPI.GetManifest(appHostCtx, &agentproto.GetManifestRequest{})
		require.NoError(t, err)

		appHost := appurl.ApplicationURL{
			Prefix:        "",
			AppSlugOrPort: "{{port}}",
			AgentName:     proxyTestAgentName,
			WorkspaceName: workspace.Name,
			Username:      me.Username,
		}
		proxyURL := "http://" + appHost.String() + strings.ReplaceAll(primaryAppHost.Host, "*", "")
		require.Equal(t, manifest.VsCodePortProxyUri, proxyURL)
		err = rpcConn.Close()
		require.NoError(t, err)
	}
	agentCloser := agent.New(agent.Options{
		Client: agentClient,
		Logger: testutil.Logger(t).Named("agent"),
	})
	t.Cleanup(func() {
		_ = agentCloser.Close()
	})

	resources := wirtualdtest.AwaitWorkspaceAgents(t, client, workspace.ID)
	agents := make([]wirtualsdk.WorkspaceAgent, 0, 1)
	for _, resource := range resources {
		agents = append(agents, resource.Agents...)
	}
	require.Len(t, agents, 1)

	return workspace, agents[0]
}

func findProtoApp(t *testing.T, protoApps []*proto.App, slug string) *proto.App {
	for _, protoApp := range protoApps {
		if protoApp.Slug == slug {
			return protoApp
		}
	}
	require.FailNowf(t, "proto app not found (slug: %q)", slug)
	return nil
}

func doWithRetries(t require.TestingT, client *wirtualsdk.Client, req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error
	require.Eventually(t, func() bool {
		// nolint // only requests which are not passed upstream have a body closed
		resp, err = client.HTTPClient.Do(req)
		if resp != nil && resp.StatusCode == http.StatusBadGateway {
			if resp.Body != nil {
				resp.Body.Close()
			}
			return false
		}
		return true
	}, testutil.WaitLong, testutil.IntervalFast)
	return resp, err
}

func requestWithRetries(ctx context.Context, t testing.TB, client *wirtualsdk.Client, method, urlOrPath string, body interface{}, opts ...wirtualsdk.RequestOption) (*http.Response, error) {
	t.Helper()
	var resp *http.Response
	var err error
	require.Eventually(t, func() bool {
		// nolint // only requests which are not passed upstream have a body closed
		resp, err = client.Request(ctx, method, urlOrPath, body, opts...)
		if resp != nil && resp.StatusCode == http.StatusBadGateway {
			if resp.Body != nil {
				resp.Body.Close()
			}
			return false
		}
		return true
	}, testutil.WaitLong, testutil.IntervalFast)
	return resp, err
}

// forceURLTransport forces the client to route all requests to the client's
// configured URLs host regardless of hostname.
func forceURLTransport(t *testing.T, client *wirtualsdk.Client) {
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	require.True(t, ok)
	transport := defaultTransport.Clone()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, client.URL.Host)
	}
	client.HTTPClient.Transport = transport
	t.Cleanup(func() {
		transport.CloseIdleConnections()
	})
}
