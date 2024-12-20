package wirtualdtest

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode"

	"cloud.google.com/go/compute/metadata"
	"github.com/fullsailor/pkcs7"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/moby/moby/pkg/namesgenerator"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"
	"google.golang.org/api/idtoken"
	"google.golang.org/api/option"
	"tailscale.com/derp"
	"tailscale.com/net/stun/stuntest"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
	"tailscale.com/types/nettype"

	"cdr.dev/slog"
	"cdr.dev/slog/sloggers/sloghuman"
	"cdr.dev/slog/sloggers/slogtest"
	"github.com/coder/quartz"
	"github.com/onchainengineering/hmi-wirtual/cryptorand"
	"github.com/onchainengineering/hmi-wirtual/provisioner/echo"
	"github.com/onchainengineering/hmi-wirtual/provisionerd"
	provisionerdproto "github.com/onchainengineering/hmi-wirtual/provisionerd/proto"
	"github.com/onchainengineering/hmi-wirtual/provisionersdk"
	sdkproto "github.com/onchainengineering/hmi-wirtual/provisionersdk/proto"
	"github.com/onchainengineering/hmi-wirtual/tailnet"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/audit"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/autobuild"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/awsidentity"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/cryptokeys"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/db2sdk"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbauthz"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbrollup"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbtestutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/pubsub"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/externalauth"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/gitsshkey"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/httpmw"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/notifications"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/notifications/notificationstest"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac/policy"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/runtimeconfig"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/schedule"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/telemetry"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/unhanger"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/updatecheck"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/util/ptr"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/workspaceapps"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/workspaceapps/appurl"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/workspacestats"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk/agentsdk"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk/drpc"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk/healthsdk"
)

type Options struct {
	// AccessURL denotes a custom access URL. By default we use the httptest
	// server's URL. Setting this may result in unexpected behavior (especially
	// with running agents).
	AccessURL                      *url.URL
	AppHostname                    string
	AWSCertificates                awsidentity.Certificates
	Authorizer                     rbac.Authorizer
	AzureCertificates              x509.VerifyOptions
	GithubOAuth2Config             *wirtuald.GithubOAuth2Config
	RealIPConfig                   *httpmw.RealIPConfig
	OIDCConfig                     *wirtuald.OIDCConfig
	GoogleTokenValidator           *idtoken.Validator
	SSHKeygenAlgorithm             gitsshkey.Algorithm
	AutobuildTicker                <-chan time.Time
	AutobuildStats                 chan<- autobuild.Stats
	Auditor                        audit.Auditor
	TLSCertificates                []tls.Certificate
	ExternalAuthConfigs            []*externalauth.Config
	TrialGenerator                 func(ctx context.Context, body wirtualsdk.LicensorTrialRequest) error
	RefreshEntitlements            func(ctx context.Context) error
	TemplateScheduleStore          schedule.TemplateScheduleStore
	Coordinator                    tailnet.Coordinator
	CoordinatorResumeTokenProvider tailnet.ResumeTokenProvider

	HealthcheckFunc    func(ctx context.Context, apiKey string) *healthsdk.HealthcheckReport
	HealthcheckTimeout time.Duration
	HealthcheckRefresh time.Duration

	// All rate limits default to -1 (unlimited) in tests if not set.
	APIRateLimit   int
	LoginRateLimit int
	FilesRateLimit int

	// OneTimePasscodeValidityPeriod specifies how long a one time passcode should be valid for.
	OneTimePasscodeValidityPeriod time.Duration

	// IncludeProvisionerDaemon when true means to start an in-memory provisionerD
	IncludeProvisionerDaemon    bool
	ProvisionerDaemonTags       map[string]string
	MetricsCacheRefreshInterval time.Duration
	AgentStatsRefreshInterval   time.Duration
	DeploymentValues            *wirtualsdk.DeploymentValues

	// Set update check options to enable update check.
	UpdateCheckOptions *updatecheck.Options

	// Overriding the database is heavily discouraged.
	// It should only be used in cases where multiple Coder
	// test instances are running against the same database.
	Database database.Store
	Pubsub   pubsub.Pubsub

	ConfigSSH wirtualsdk.SSHConfigResponse

	SwaggerEndpoint bool
	// Logger should only be overridden if you expect errors
	// as part of your test.
	Logger       *slog.Logger
	StatsBatcher workspacestats.Batcher

	WorkspaceAppsStatsCollectorOptions workspaceapps.StatsCollectorOptions
	AllowWorkspaceRenames              bool
	NewTicker                          func(duration time.Duration) (<-chan time.Time, func())
	DatabaseRolluper                   *dbrollup.Rolluper
	WorkspaceUsageTrackerFlush         chan int
	WorkspaceUsageTrackerTick          chan time.Time
	NotificationsEnqueuer              notifications.Enqueuer
	APIKeyEncryptionCache              cryptokeys.EncryptionKeycache
	OIDCConvertKeyCache                cryptokeys.SigningKeycache
	Clock                              quartz.Clock
}

// New constructs a wirtualsdk client connected to an in-memory API instance.
func New(t testing.TB, options *Options) *wirtualsdk.Client {
	client, _ := newWithCloser(t, options)
	return client
}

// NewWithDatabase constructs a wirtualsdk client connected to an in-memory API instance.
// The database is returned to provide direct data manipulation for tests.
func NewWithDatabase(t testing.TB, options *Options) (*wirtualsdk.Client, database.Store) {
	client, _, api := NewWithAPI(t, options)
	return client, api.Database
}

// NewWithProvisionerCloser returns a client as well as a handle to close
// the provisioner. This is a temporary function while work is done to
// standardize how provisioners are registered with wirtuald. The option
// to include a provisioner is set to true for convenience.
func NewWithProvisionerCloser(t testing.TB, options *Options) (*wirtualsdk.Client, io.Closer) {
	if options == nil {
		options = &Options{}
	}
	options.IncludeProvisionerDaemon = true
	client, closer := newWithCloser(t, options)
	return client, closer
}

// newWithCloser constructs a wirtualsdk client connected to an in-memory API instance.
// The returned closer closes a provisioner if it was provided
// The API is intentionally not returned here because wirtuald tests should not
// require a handle to the API. Do not expose the API or wrath shall descend
// upon thee. Even the io.Closer that is exposed here shouldn't be exposed
// and is a temporary measure while the API to register provisioners is ironed
// out.
func newWithCloser(t testing.TB, options *Options) (*wirtualsdk.Client, io.Closer) {
	client, closer, _ := NewWithAPI(t, options)
	return client, closer
}

func NewOptions(t testing.TB, options *Options) (func(http.Handler), context.CancelFunc, *url.URL, *wirtuald.Options) {
	t.Helper()

	if options == nil {
		options = &Options{}
	}
	if options.Logger == nil {
		logger := slogtest.Make(t, &slogtest.Options{IgnoreErrors: true}).Leveled(slog.LevelDebug).Named("wirtuald")
		options.Logger = &logger
	}
	if options.GoogleTokenValidator == nil {
		ctx, cancelFunc := context.WithCancel(context.Background())
		t.Cleanup(cancelFunc)
		var err error
		options.GoogleTokenValidator, err = idtoken.NewValidator(ctx, option.WithoutAuthentication())
		require.NoError(t, err)
	}
	if options.AutobuildTicker == nil {
		ticker := make(chan time.Time)
		options.AutobuildTicker = ticker
		t.Cleanup(func() { close(ticker) })
	}
	if options.AutobuildStats != nil {
		t.Cleanup(func() {
			close(options.AutobuildStats)
		})
	}

	if options.Authorizer == nil {
		defAuth := rbac.NewStrictCachingAuthorizer(prometheus.NewRegistry())
		if _, ok := t.(*testing.T); ok {
			options.Authorizer = &RecordingAuthorizer{
				Wrapped: defAuth,
			}
		} else {
			// In benchmarks, the recording authorizer greatly skews results.
			options.Authorizer = defAuth
		}
	}

	if options.Database == nil {
		options.Database, options.Pubsub = dbtestutil.NewDB(t)
	}
	if options.CoordinatorResumeTokenProvider == nil {
		options.CoordinatorResumeTokenProvider = tailnet.NewInsecureTestResumeTokenProvider()
	}

	if options.NotificationsEnqueuer == nil {
		options.NotificationsEnqueuer = &notificationstest.FakeEnqueuer{}
	}

	accessControlStore := &atomic.Pointer[dbauthz.AccessControlStore]{}
	var acs dbauthz.AccessControlStore = dbauthz.AGPLTemplateAccessControlStore{}
	accessControlStore.Store(&acs)

	runtimeManager := runtimeconfig.NewManager()
	options.Database = dbauthz.New(options.Database, options.Authorizer, *options.Logger, accessControlStore)

	// Some routes expect a deployment ID, so just make sure one exists.
	// Check first incase the caller already set up this database.
	// nolint:gocritic // Setting up unit test data inside test helper
	depID, err := options.Database.GetDeploymentID(dbauthz.AsSystemRestricted(context.Background()))
	if xerrors.Is(err, sql.ErrNoRows) || depID == "" {
		// nolint:gocritic // Setting up unit test data inside test helper
		err := options.Database.InsertDeploymentID(dbauthz.AsSystemRestricted(context.Background()), uuid.NewString())
		require.NoError(t, err, "insert a deployment id")
	}

	if options.DeploymentValues == nil {
		options.DeploymentValues = DeploymentValues(t)
	}
	// DisableOwnerWorkspaceExec modifies the 'global' RBAC roles. Fast-fail tests if we detect this.
	if !options.DeploymentValues.DisableOwnerWorkspaceExec.Value() {
		ownerSubj := rbac.Subject{
			Roles: rbac.RoleIdentifiers{rbac.RoleOwner()},
			Scope: rbac.ScopeAll,
		}
		if err := options.Authorizer.Authorize(context.Background(), ownerSubj, policy.ActionSSH, rbac.ResourceWorkspace); err != nil {
			if rbac.IsUnauthorizedError(err) {
				t.Fatal("Side-effect of DisableOwnerWorkspaceExec detected in unrelated test. Please move the test that requires DisableOwnerWorkspaceExec to its own package so that it does not impact other tests!")
			}
			require.NoError(t, err)
		}
	}

	// If no ratelimits are set, disable all rate limiting for tests.
	if options.APIRateLimit == 0 {
		options.APIRateLimit = -1
	}
	if options.LoginRateLimit == 0 {
		options.LoginRateLimit = -1
	}
	if options.FilesRateLimit == 0 {
		options.FilesRateLimit = -1
	}
	if options.StatsBatcher == nil {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		batcher, closeBatcher, err := workspacestats.NewBatcher(ctx,
			workspacestats.BatcherWithStore(options.Database),
			// Avoid cluttering up test output.
			workspacestats.BatcherWithLogger(slog.Make(sloghuman.Sink(io.Discard))),
		)
		require.NoError(t, err, "create stats batcher")
		options.StatsBatcher = batcher
		t.Cleanup(closeBatcher)
	}
	if options.NotificationsEnqueuer == nil {
		options.NotificationsEnqueuer = &notificationstest.FakeEnqueuer{}
	}

	if options.OneTimePasscodeValidityPeriod == 0 {
		options.OneTimePasscodeValidityPeriod = testutil.WaitLong
	}

	var templateScheduleStore atomic.Pointer[schedule.TemplateScheduleStore]
	if options.TemplateScheduleStore == nil {
		options.TemplateScheduleStore = schedule.NewAGPLTemplateScheduleStore()
	}
	templateScheduleStore.Store(&options.TemplateScheduleStore)

	var auditor atomic.Pointer[audit.Auditor]
	if options.Auditor == nil {
		options.Auditor = audit.NewNop()
	}
	auditor.Store(&options.Auditor)

	ctx, cancelFunc := context.WithCancel(context.Background())
	lifecycleExecutor := autobuild.NewExecutor(
		ctx,
		options.Database,
		options.Pubsub,
		prometheus.NewRegistry(),
		&templateScheduleStore,
		&auditor,
		accessControlStore,
		*options.Logger,
		options.AutobuildTicker,
		options.NotificationsEnqueuer,
	).WithStatsChannel(options.AutobuildStats)
	lifecycleExecutor.Run()

	hangDetectorTicker := time.NewTicker(options.DeploymentValues.JobHangDetectorInterval.Value())
	defer hangDetectorTicker.Stop()
	hangDetector := unhanger.New(ctx, options.Database, options.Pubsub, options.Logger.Named("unhanger.detector"), hangDetectorTicker.C)
	hangDetector.Start()
	t.Cleanup(hangDetector.Close)

	// Did last_used_at not update? Scratching your noggin? Here's why.
	// Workspace usage tracking must be triggered manually in tests.
	// The vast majority of existing tests do not depend on last_used_at
	// and adding an extra time-based background goroutine to all existing
	// tests may lead to future flakes and goleak complaints.
	// Instead, pass in your own flush and ticker like so:
	//
	//   tickCh = make(chan time.Time)
	//   flushCh = make(chan int, 1)
	//   client  = wirtualdtest.New(t, &wirtualdtest.Options{
	//     WorkspaceUsageTrackerFlush: flushCh,
	//     WorkspaceUsageTrackerTick: tickCh
	//   })
	//
	// Now to trigger a tick, just write to `tickCh`.
	// Reading from `flushCh` will ensure that workspaceusage.Tracker flushed.
	// See TestPortForward or TestTracker_MultipleInstances for how this works in practice.
	if options.WorkspaceUsageTrackerFlush == nil {
		options.WorkspaceUsageTrackerFlush = make(chan int, 1) // buffering just in case
	}
	if options.WorkspaceUsageTrackerTick == nil {
		options.WorkspaceUsageTrackerTick = make(chan time.Time, 1) // buffering just in case
	}
	// Close is called by API.Close()
	wuTracker := workspacestats.NewTracker(
		options.Database,
		workspacestats.TrackerWithLogger(options.Logger.Named("workspace_usage_tracker")),
		workspacestats.TrackerWithTickFlush(options.WorkspaceUsageTrackerTick, options.WorkspaceUsageTrackerFlush),
	)

	var mutex sync.RWMutex
	var handler http.Handler
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutex.RLock()
		handler := handler
		mutex.RUnlock()
		if handler != nil {
			handler.ServeHTTP(w, r)
		}
	}))
	srv.Config.BaseContext = func(_ net.Listener) context.Context {
		return ctx
	}
	if options.TLSCertificates != nil {
		srv.TLS = &tls.Config{
			Certificates: options.TLSCertificates,
			MinVersion:   tls.VersionTLS12,
		}
		srv.StartTLS()
	} else {
		srv.Start()
	}
	t.Cleanup(srv.Close)

	tcpAddr, ok := srv.Listener.Addr().(*net.TCPAddr)
	require.True(t, ok)

	serverURL, err := url.Parse(srv.URL)
	require.NoError(t, err)
	serverURL.Host = fmt.Sprintf("localhost:%d", tcpAddr.Port)

	derpPort, err := strconv.Atoi(serverURL.Port())
	require.NoError(t, err)

	accessURL := options.AccessURL
	if accessURL == nil {
		accessURL = serverURL
	}

	// If the STUNAddresses setting is empty or the default, start a STUN
	// server. Otherwise, use the value as is.
	var (
		stunAddresses   []string
		dvStunAddresses = options.DeploymentValues.DERP.Server.STUNAddresses.Value()
	)
	if len(dvStunAddresses) == 0 || dvStunAddresses[0] == "stun.l.google.com:19302" {
		stunAddr, stunCleanup := stuntest.ServeWithPacketListener(t, nettype.Std{})
		stunAddr.IP = net.ParseIP("127.0.0.1")
		t.Cleanup(stunCleanup)
		stunAddresses = []string{stunAddr.String()}
		options.DeploymentValues.DERP.Server.STUNAddresses = stunAddresses
	} else if dvStunAddresses[0] != tailnet.DisableSTUN {
		stunAddresses = options.DeploymentValues.DERP.Server.STUNAddresses.Value()
	}

	derpServer := derp.NewServer(key.NewNode(), tailnet.Logger(options.Logger.Named("derp").Leveled(slog.LevelDebug)))
	derpServer.SetMeshKey("test-key")

	// match default with cli default
	if options.SSHKeygenAlgorithm == "" {
		options.SSHKeygenAlgorithm = gitsshkey.AlgorithmEd25519
	}

	var appHostnameRegex *regexp.Regexp
	if options.AppHostname != "" {
		var err error
		appHostnameRegex, err = appurl.CompileHostnamePattern(options.AppHostname)
		require.NoError(t, err)
	}

	region := &tailcfg.DERPRegion{
		EmbeddedRelay: true,
		RegionID:      int(options.DeploymentValues.DERP.Server.RegionID.Value()),
		RegionCode:    options.DeploymentValues.DERP.Server.RegionCode.String(),
		RegionName:    options.DeploymentValues.DERP.Server.RegionName.String(),
		Nodes: []*tailcfg.DERPNode{{
			Name:     fmt.Sprintf("%db", options.DeploymentValues.DERP.Server.RegionID),
			RegionID: int(options.DeploymentValues.DERP.Server.RegionID.Value()),
			IPv4:     "127.0.0.1",
			DERPPort: derpPort,
			// STUN port is added as a separate node by tailnet.NewDERPMap() if
			// direct connections are enabled.
			STUNPort:         -1,
			InsecureForTests: true,
			ForceHTTP:        options.TLSCertificates == nil,
		}},
	}
	if !options.DeploymentValues.DERP.Server.Enable.Value() {
		region = nil
	}
	derpMap, err := tailnet.NewDERPMap(ctx, region, stunAddresses,
		options.DeploymentValues.DERP.Config.URL.Value(),
		options.DeploymentValues.DERP.Config.Path.Value(),
		options.DeploymentValues.DERP.Config.BlockDirect.Value(),
	)
	require.NoError(t, err)

	return func(h http.Handler) {
			mutex.Lock()
			defer mutex.Unlock()
			handler = h
		}, cancelFunc, serverURL, &wirtuald.Options{
			AgentConnectionUpdateFrequency: 150 * time.Millisecond,
			// Force a long disconnection timeout to ensure
			// agents are not marked as disconnected during slow tests.
			AgentInactiveDisconnectTimeout: testutil.WaitShort,
			AccessURL:                      accessURL,
			AppHostname:                    options.AppHostname,
			AppHostnameRegex:               appHostnameRegex,
			Logger:                         *options.Logger,
			CacheDir:                       t.TempDir(),
			RuntimeConfig:                  runtimeManager,
			Database:                       options.Database,
			Pubsub:                         options.Pubsub,
			ExternalAuthConfigs:            options.ExternalAuthConfigs,

			Auditor:                            options.Auditor,
			AWSCertificates:                    options.AWSCertificates,
			AzureCertificates:                  options.AzureCertificates,
			GithubOAuth2Config:                 options.GithubOAuth2Config,
			RealIPConfig:                       options.RealIPConfig,
			OIDCConfig:                         options.OIDCConfig,
			GoogleTokenValidator:               options.GoogleTokenValidator,
			SSHKeygenAlgorithm:                 options.SSHKeygenAlgorithm,
			DERPServer:                         derpServer,
			APIRateLimit:                       options.APIRateLimit,
			LoginRateLimit:                     options.LoginRateLimit,
			FilesRateLimit:                     options.FilesRateLimit,
			Authorizer:                         options.Authorizer,
			Telemetry:                          telemetry.NewNoop(),
			TemplateScheduleStore:              &templateScheduleStore,
			AccessControlStore:                 accessControlStore,
			TLSCertificates:                    options.TLSCertificates,
			TrialGenerator:                     options.TrialGenerator,
			RefreshEntitlements:                options.RefreshEntitlements,
			TailnetCoordinator:                 options.Coordinator,
			BaseDERPMap:                        derpMap,
			DERPMapUpdateFrequency:             150 * time.Millisecond,
			CoordinatorResumeTokenProvider:     options.CoordinatorResumeTokenProvider,
			MetricsCacheRefreshInterval:        options.MetricsCacheRefreshInterval,
			AgentStatsRefreshInterval:          options.AgentStatsRefreshInterval,
			DeploymentValues:                   options.DeploymentValues,
			DeploymentOptions:                  wirtualsdk.DeploymentOptionsWithoutSecrets(options.DeploymentValues.Options()),
			UpdateCheckOptions:                 options.UpdateCheckOptions,
			SwaggerEndpoint:                    options.SwaggerEndpoint,
			SSHConfig:                          options.ConfigSSH,
			HealthcheckFunc:                    options.HealthcheckFunc,
			HealthcheckTimeout:                 options.HealthcheckTimeout,
			HealthcheckRefresh:                 options.HealthcheckRefresh,
			StatsBatcher:                       options.StatsBatcher,
			WorkspaceAppsStatsCollectorOptions: options.WorkspaceAppsStatsCollectorOptions,
			AllowWorkspaceRenames:              options.AllowWorkspaceRenames,
			NewTicker:                          options.NewTicker,
			DatabaseRolluper:                   options.DatabaseRolluper,
			WorkspaceUsageTracker:              wuTracker,
			NotificationsEnqueuer:              options.NotificationsEnqueuer,
			OneTimePasscodeValidityPeriod:      options.OneTimePasscodeValidityPeriod,
			Clock:                              options.Clock,
			AppEncryptionKeyCache:              options.APIKeyEncryptionCache,
			OIDCConvertKeyCache:                options.OIDCConvertKeyCache,
		}
}

// NewWithAPI constructs an in-memory API instance and returns a client to talk to it.
// Most tests never need a reference to the API, but AuthorizationTest in this module uses it.
// Do not expose the API or wrath shall descend upon thee.
func NewWithAPI(t testing.TB, options *Options) (*wirtualsdk.Client, io.Closer, *wirtuald.API) {
	if options == nil {
		options = &Options{}
	}
	setHandler, cancelFunc, serverURL, newOptions := NewOptions(t, options)
	// We set the handler after server creation for the access URL.
	coderAPI := wirtuald.New(newOptions)
	setHandler(coderAPI.RootHandler)
	var provisionerCloser io.Closer = nopcloser{}
	if options.IncludeProvisionerDaemon {
		provisionerCloser = NewTaggedProvisionerDaemon(t, coderAPI, "test", options.ProvisionerDaemonTags)
	}
	client := wirtualsdk.New(serverURL)
	t.Cleanup(func() {
		cancelFunc()
		_ = provisionerCloser.Close()
		_ = coderAPI.Close()
		client.HTTPClient.CloseIdleConnections()
	})
	return client, provisionerCloser, coderAPI
}

// ProvisionerdCloser wraps a provisioner daemon as an io.Closer that can be called multiple times
type ProvisionerdCloser struct {
	mu     sync.Mutex
	closed bool
	d      *provisionerd.Server
}

func NewProvisionerDaemonCloser(d *provisionerd.Server) *ProvisionerdCloser {
	return &ProvisionerdCloser{d: d}
}

func (c *ProvisionerdCloser) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitShort)
	defer cancel()
	shutdownErr := c.d.Shutdown(ctx, true)
	closeErr := c.d.Close()
	if shutdownErr != nil {
		return shutdownErr
	}
	return closeErr
}

// NewProvisionerDaemon launches a provisionerd instance configured to work
// well with wirtuald testing. It registers the "echo" provisioner for
// quick testing.
func NewProvisionerDaemon(t testing.TB, coderAPI *wirtuald.API) io.Closer {
	return NewTaggedProvisionerDaemon(t, coderAPI, "test", nil)
}

func NewTaggedProvisionerDaemon(t testing.TB, coderAPI *wirtuald.API, name string, provisionerTags map[string]string) io.Closer {
	t.Helper()

	// t.Cleanup runs in last added, first called order. t.TempDir() will delete
	// the directory on cleanup, so we want to make sure the echoServer is closed
	// before we go ahead an attempt to delete it's work directory.
	// seems t.TempDir() is not safe to call from a different goroutine
	workDir := t.TempDir()

	echoClient, echoServer := drpc.MemTransportPipe()
	ctx, cancelFunc := context.WithCancel(context.Background())
	t.Cleanup(func() {
		_ = echoClient.Close()
		_ = echoServer.Close()
		cancelFunc()
	})

	go func() {
		err := echo.Serve(ctx, &provisionersdk.ServeOptions{
			Listener:      echoServer,
			WorkDirectory: workDir,
			Logger:        coderAPI.Logger.Named("echo").Leveled(slog.LevelDebug),
		})
		assert.NoError(t, err)
	}()

	daemon := provisionerd.New(func(dialCtx context.Context) (provisionerdproto.DRPCProvisionerDaemonClient, error) {
		return coderAPI.CreateInMemoryTaggedProvisionerDaemon(dialCtx, name, []wirtualsdk.ProvisionerType{wirtualsdk.ProvisionerTypeEcho}, provisionerTags)
	}, &provisionerd.Options{
		Logger:              coderAPI.Logger.Named("provisionerd").Leveled(slog.LevelDebug),
		UpdateInterval:      250 * time.Millisecond,
		ForceCancelInterval: 5 * time.Second,
		Connector: provisionerd.LocalProvisioners{
			string(database.ProvisionerTypeEcho): sdkproto.NewDRPCProvisionerClient(echoClient),
		},
	})
	closer := NewProvisionerDaemonCloser(daemon)
	t.Cleanup(func() {
		_ = closer.Close()
	})
	return closer
}

var FirstUserParams = wirtualsdk.CreateFirstUserRequest{
	Email:    "testuser@coder.com",
	Username: "testuser",
	Password: "SomeSecurePassword!",
	Name:     "Test User",
}

var TrialUserParams = wirtualsdk.CreateFirstUserTrialInfo{
	FirstName:   "John",
	LastName:    "Doe",
	PhoneNumber: "9999999999",
	JobTitle:    "Engineer",
	CompanyName: "Acme Inc",
	Country:     "United States",
	Developers:  "10-50",
}

// CreateFirstUser creates a user with preset credentials and authenticates
// with the passed in wirtualsdk client.
func CreateFirstUser(t testing.TB, client *wirtualsdk.Client) wirtualsdk.CreateFirstUserResponse {
	resp, err := client.CreateFirstUser(context.Background(), FirstUserParams)
	require.NoError(t, err)

	login, err := client.LoginWithPassword(context.Background(), wirtualsdk.LoginWithPasswordRequest{
		Email:    FirstUserParams.Email,
		Password: FirstUserParams.Password,
	})
	require.NoError(t, err)
	client.SetSessionToken(login.SessionToken)
	return resp
}

// CreateAnotherUser creates and authenticates a new user.
// Roles can include org scoped roles with 'roleName:<organization_id>'
func CreateAnotherUser(t testing.TB, client *wirtualsdk.Client, organizationID uuid.UUID, roles ...rbac.RoleIdentifier) (*wirtualsdk.Client, wirtualsdk.User) {
	return createAnotherUserRetry(t, client, []uuid.UUID{organizationID}, 5, roles)
}

func CreateAnotherUserMutators(t testing.TB, client *wirtualsdk.Client, organizationID uuid.UUID, roles []rbac.RoleIdentifier, mutators ...func(r *wirtualsdk.CreateUserRequestWithOrgs)) (*wirtualsdk.Client, wirtualsdk.User) {
	return createAnotherUserRetry(t, client, []uuid.UUID{organizationID}, 5, roles, mutators...)
}

// AuthzUserSubject does not include the user's groups.
func AuthzUserSubject(user wirtualsdk.User, orgID uuid.UUID) rbac.Subject {
	roles := make(rbac.RoleIdentifiers, 0, len(user.Roles))
	// Member role is always implied
	roles = append(roles, rbac.RoleMember())
	for _, r := range user.Roles {
		orgID, _ := uuid.Parse(r.OrganizationID) // defaults to nil
		roles = append(roles, rbac.RoleIdentifier{
			Name:           r.Name,
			OrganizationID: orgID,
		})
	}
	// We assume only 1 org exists
	roles = append(roles, rbac.ScopedRoleOrgMember(orgID))

	return rbac.Subject{
		ID:     user.ID.String(),
		Roles:  roles,
		Groups: []string{},
		Scope:  rbac.ScopeAll,
	}
}

func createAnotherUserRetry(t testing.TB, client *wirtualsdk.Client, organizationIDs []uuid.UUID, retries int, roles []rbac.RoleIdentifier, mutators ...func(r *wirtualsdk.CreateUserRequestWithOrgs)) (*wirtualsdk.Client, wirtualsdk.User) {
	req := wirtualsdk.CreateUserRequestWithOrgs{
		Email:           namesgenerator.GetRandomName(10) + "@coder.com",
		Username:        RandomUsername(t),
		Name:            RandomName(t),
		Password:        "SomeSecurePassword!",
		OrganizationIDs: organizationIDs,
		// Always create users as active in tests to ignore an extra audit log
		// when logging in.
		UserStatus: ptr.Ref(wirtualsdk.UserStatusActive),
	}
	for _, m := range mutators {
		m(&req)
	}

	user, err := client.CreateUserWithOrgs(context.Background(), req)
	var apiError *wirtualsdk.Error
	// If the user already exists by username or email conflict, try again up to "retries" times.
	if err != nil && retries >= 0 && xerrors.As(err, &apiError) {
		if apiError.StatusCode() == http.StatusConflict {
			retries--
			return createAnotherUserRetry(t, client, organizationIDs, retries, roles)
		}
	}
	require.NoError(t, err)

	var sessionToken string
	if req.UserLoginType == wirtualsdk.LoginTypeNone {
		// Cannot log in with a disabled login user. So make it an api key from
		// the client making this user.
		token, err := client.CreateToken(context.Background(), user.ID.String(), wirtualsdk.CreateTokenRequest{
			Lifetime:  time.Hour * 24,
			Scope:     wirtualsdk.APIKeyScopeAll,
			TokenName: "no-password-user-token",
		})
		require.NoError(t, err)
		sessionToken = token.Key
	} else {
		login, err := client.LoginWithPassword(context.Background(), wirtualsdk.LoginWithPasswordRequest{
			Email:    req.Email,
			Password: req.Password,
		})
		require.NoError(t, err)
		sessionToken = login.SessionToken
	}

	if user.Status == wirtualsdk.UserStatusDormant {
		// Use admin client so that user's LastSeenAt is not updated.
		// In general we need to refresh the user status, which should
		// transition from "dormant" to "active".
		user, err = client.User(context.Background(), user.Username)
		require.NoError(t, err)
	}

	other := wirtualsdk.New(client.URL)
	other.SetSessionToken(sessionToken)
	t.Cleanup(func() {
		other.HTTPClient.CloseIdleConnections()
	})

	if len(roles) > 0 {
		// Find the roles for the org vs the site wide roles
		orgRoles := make(map[uuid.UUID][]rbac.RoleIdentifier)
		var siteRoles []rbac.RoleIdentifier

		for _, roleName := range roles {
			ok := roleName.IsOrgRole()
			if ok {
				orgRoles[roleName.OrganizationID] = append(orgRoles[roleName.OrganizationID], roleName)
			} else {
				siteRoles = append(siteRoles, roleName)
			}
		}
		// Update the roles
		for _, r := range user.Roles {
			orgID, _ := uuid.Parse(r.OrganizationID)
			siteRoles = append(siteRoles, rbac.RoleIdentifier{
				Name:           r.Name,
				OrganizationID: orgID,
			})
		}

		onlyName := func(role rbac.RoleIdentifier) string {
			return role.Name
		}

		user, err = client.UpdateUserRoles(context.Background(), user.ID.String(), wirtualsdk.UpdateRoles{Roles: db2sdk.List(siteRoles, onlyName)})
		require.NoError(t, err, "update site roles")

		// isMember keeps track of which orgs the user was added to as a member
		isMember := make(map[uuid.UUID]bool)
		for _, orgID := range organizationIDs {
			isMember[orgID] = true
		}

		// Update org roles
		for orgID, roles := range orgRoles {
			// The user must be an organization of any orgRoles, so insert
			// the organization member, then assign the roles.
			if !isMember[orgID] {
				_, err = client.PostOrganizationMember(context.Background(), orgID, user.ID.String())
				require.NoError(t, err, "add user to organization as member")
			}

			_, err = client.UpdateOrganizationMemberRoles(context.Background(), orgID, user.ID.String(),
				wirtualsdk.UpdateRoles{Roles: db2sdk.List(roles, onlyName)})
			require.NoError(t, err, "update org membership roles")
			isMember[orgID] = true
		}
	}

	user, err = client.User(context.Background(), user.Username)
	require.NoError(t, err, "update final user")

	return other, user
}

// CreateTemplateVersion creates a template import provisioner job
// with the responses provided. It uses the "echo" provisioner for compatibility
// with testing.
func CreateTemplateVersion(t testing.TB, client *wirtualsdk.Client, organizationID uuid.UUID, res *echo.Responses, mutators ...func(*wirtualsdk.CreateTemplateVersionRequest)) wirtualsdk.TemplateVersion {
	t.Helper()
	data, err := echo.TarWithOptions(context.Background(), client.Logger(), res)
	require.NoError(t, err)
	file, err := client.Upload(context.Background(), wirtualsdk.ContentTypeTar, bytes.NewReader(data))
	require.NoError(t, err)

	req := wirtualsdk.CreateTemplateVersionRequest{
		FileID:        file.ID,
		StorageMethod: wirtualsdk.ProvisionerStorageMethodFile,
		Provisioner:   wirtualsdk.ProvisionerTypeEcho,
	}
	for _, mut := range mutators {
		mut(&req)
	}

	templateVersion, err := client.CreateTemplateVersion(context.Background(), organizationID, req)
	require.NoError(t, err)
	return templateVersion
}

// CreateWorkspaceBuild creates a workspace build for the given workspace and transition.
func CreateWorkspaceBuild(
	t *testing.T,
	client *wirtualsdk.Client,
	workspace wirtualsdk.Workspace,
	transition database.WorkspaceTransition,
	mutators ...func(*wirtualsdk.CreateWorkspaceBuildRequest),
) wirtualsdk.WorkspaceBuild {
	t.Helper()

	req := wirtualsdk.CreateWorkspaceBuildRequest{
		Transition: wirtualsdk.WorkspaceTransition(transition),
	}
	for _, mut := range mutators {
		mut(&req)
	}
	build, err := client.CreateWorkspaceBuild(context.Background(), workspace.ID, req)
	require.NoError(t, err)
	return build
}

// CreateTemplate creates a template with the "echo" provisioner for
// compatibility with testing. The name assigned is randomly generated.
func CreateTemplate(t testing.TB, client *wirtualsdk.Client, organization uuid.UUID, version uuid.UUID, mutators ...func(*wirtualsdk.CreateTemplateRequest)) wirtualsdk.Template {
	req := wirtualsdk.CreateTemplateRequest{
		Name:      RandomUsername(t),
		VersionID: version,
	}
	for _, mut := range mutators {
		mut(&req)
	}
	template, err := client.CreateTemplate(context.Background(), organization, req)
	require.NoError(t, err)
	return template
}

// CreateGroup creates a group with the given name and members.
func CreateGroup(t testing.TB, client *wirtualsdk.Client, organizationID uuid.UUID, name string, members ...wirtualsdk.User) wirtualsdk.Group {
	t.Helper()
	group, err := client.CreateGroup(context.Background(), organizationID, wirtualsdk.CreateGroupRequest{
		Name: name,
	})
	require.NoError(t, err, "failed to create group")
	memberIDs := make([]string, 0)
	for _, member := range members {
		memberIDs = append(memberIDs, member.ID.String())
	}
	group, err = client.PatchGroup(context.Background(), group.ID, wirtualsdk.PatchGroupRequest{
		AddUsers: memberIDs,
	})

	require.NoError(t, err, "failed to add members to group")
	return group
}

// UpdateTemplateVersion creates a new template version with the "echo" provisioner
// and associates it with the given templateID.
func UpdateTemplateVersion(t testing.TB, client *wirtualsdk.Client, organizationID uuid.UUID, res *echo.Responses, templateID uuid.UUID) wirtualsdk.TemplateVersion {
	ctx := context.Background()
	data, err := echo.Tar(res)
	require.NoError(t, err)
	file, err := client.Upload(ctx, wirtualsdk.ContentTypeTar, bytes.NewReader(data))
	require.NoError(t, err)
	templateVersion, err := client.CreateTemplateVersion(ctx, organizationID, wirtualsdk.CreateTemplateVersionRequest{
		TemplateID:    templateID,
		FileID:        file.ID,
		StorageMethod: wirtualsdk.ProvisionerStorageMethodFile,
		Provisioner:   wirtualsdk.ProvisionerTypeEcho,
	})
	require.NoError(t, err)
	return templateVersion
}

func UpdateActiveTemplateVersion(t testing.TB, client *wirtualsdk.Client, templateID, versionID uuid.UUID) {
	err := client.UpdateActiveTemplateVersion(context.Background(), templateID, wirtualsdk.UpdateActiveTemplateVersion{
		ID: versionID,
	})
	require.NoError(t, err)
}

// UpdateTemplateMeta updates the template meta for the given template.
func UpdateTemplateMeta(t testing.TB, client *wirtualsdk.Client, templateID uuid.UUID, meta wirtualsdk.UpdateTemplateMeta) wirtualsdk.Template {
	t.Helper()
	updated, err := client.UpdateTemplateMeta(context.Background(), templateID, meta)
	require.NoError(t, err)
	return updated
}

// AwaitTemplateVersionJobRunning waits for the build to be picked up by a provisioner.
func AwaitTemplateVersionJobRunning(t testing.TB, client *wirtualsdk.Client, version uuid.UUID) wirtualsdk.TemplateVersion {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitShort)
	defer cancel()

	t.Logf("waiting for template version %s build job to start", version)
	var templateVersion wirtualsdk.TemplateVersion
	require.Eventually(t, func() bool {
		var err error
		templateVersion, err = client.TemplateVersion(ctx, version)
		if err != nil {
			return false
		}
		t.Logf("template version job status: %s", templateVersion.Job.Status)
		switch templateVersion.Job.Status {
		case wirtualsdk.ProvisionerJobPending:
			return false
		case wirtualsdk.ProvisionerJobRunning:
			return true
		default:
			t.FailNow()
			return false
		}
	}, testutil.WaitShort, testutil.IntervalFast, "make sure you set `IncludeProvisionerDaemon`!")
	t.Logf("template version %s job has started", version)
	return templateVersion
}

// AwaitTemplateVersionJobCompleted waits for the build to be completed. This may result
// from cancelation, an error, or from completing successfully.
func AwaitTemplateVersionJobCompleted(t testing.TB, client *wirtualsdk.Client, version uuid.UUID) wirtualsdk.TemplateVersion {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()

	t.Logf("waiting for template version %s build job to complete", version)
	var templateVersion wirtualsdk.TemplateVersion
	require.Eventually(t, func() bool {
		var err error
		templateVersion, err = client.TemplateVersion(ctx, version)
		t.Logf("template version job status: %s", templateVersion.Job.Status)
		return assert.NoError(t, err) && templateVersion.Job.CompletedAt != nil
	}, testutil.WaitLong, testutil.IntervalMedium, "make sure you set `IncludeProvisionerDaemon`!")
	t.Logf("template version %s job has completed", version)
	return templateVersion
}

// AwaitWorkspaceBuildJobCompleted waits for a workspace provision job to reach completed status.
func AwaitWorkspaceBuildJobCompleted(t testing.TB, client *wirtualsdk.Client, build uuid.UUID) wirtualsdk.WorkspaceBuild {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitShort)
	defer cancel()

	t.Logf("waiting for workspace build job %s", build)
	var workspaceBuild wirtualsdk.WorkspaceBuild
	require.Eventually(t, func() bool {
		var err error
		workspaceBuild, err = client.WorkspaceBuild(ctx, build)
		return assert.NoError(t, err) && workspaceBuild.Job.CompletedAt != nil
	}, testutil.WaitMedium, testutil.IntervalMedium)
	t.Logf("got workspace build job %s", build)
	return workspaceBuild
}

// AwaitWorkspaceAgents waits for all resources with agents to be connected. If
// specific agents are provided, it will wait for those agents to be connected
// but will not fail if other agents are not connected.
//
// Deprecated: Use NewWorkspaceAgentWaiter
func AwaitWorkspaceAgents(t testing.TB, client *wirtualsdk.Client, workspaceID uuid.UUID, agentNames ...string) []wirtualsdk.WorkspaceResource {
	return NewWorkspaceAgentWaiter(t, client, workspaceID).AgentNames(agentNames).Wait()
}

// WorkspaceAgentWaiter waits for all resources with agents to be connected. If
// specific agents are provided using AgentNames(), it will wait for those agents
// to be connected but will not fail if other agents are not connected.
type WorkspaceAgentWaiter struct {
	t                testing.TB
	client           *wirtualsdk.Client
	workspaceID      uuid.UUID
	agentNames       []string
	resourcesMatcher func([]wirtualsdk.WorkspaceResource) bool
}

// NewWorkspaceAgentWaiter returns an object that waits for agents to connect when
// you call Wait() on it.
func NewWorkspaceAgentWaiter(t testing.TB, client *wirtualsdk.Client, workspaceID uuid.UUID) WorkspaceAgentWaiter {
	return WorkspaceAgentWaiter{
		t:           t,
		client:      client,
		workspaceID: workspaceID,
	}
}

// AgentNames instructs the waiter to wait for the given, named agents to be connected and will
// return even if other agents are not connected.
func (w WorkspaceAgentWaiter) AgentNames(names []string) WorkspaceAgentWaiter {
	//nolint: revive // returns modified struct
	w.agentNames = names
	return w
}

// MatchResources instructs the waiter to wait until the workspace has resources that cause the
// provided matcher function to return true.
func (w WorkspaceAgentWaiter) MatchResources(m func([]wirtualsdk.WorkspaceResource) bool) WorkspaceAgentWaiter {
	//nolint: revive // returns modified struct
	w.resourcesMatcher = m
	return w
}

// Wait waits for the agent(s) to connect and fails the test if they do not within testutil.WaitLong
func (w WorkspaceAgentWaiter) Wait() []wirtualsdk.WorkspaceResource {
	w.t.Helper()

	agentNamesMap := make(map[string]struct{}, len(w.agentNames))
	for _, name := range w.agentNames {
		agentNamesMap[name] = struct{}{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()

	w.t.Logf("waiting for workspace agents (workspace %s)", w.workspaceID)
	var resources []wirtualsdk.WorkspaceResource
	require.Eventually(w.t, func() bool {
		var err error
		workspace, err := w.client.Workspace(ctx, w.workspaceID)
		if err != nil {
			return false
		}
		if workspace.LatestBuild.Job.CompletedAt == nil {
			return false
		}
		if workspace.LatestBuild.Job.CompletedAt.IsZero() {
			return false
		}

		for _, resource := range workspace.LatestBuild.Resources {
			for _, agent := range resource.Agents {
				if len(w.agentNames) > 0 {
					if _, ok := agentNamesMap[agent.Name]; !ok {
						continue
					}
				}

				if agent.Status != wirtualsdk.WorkspaceAgentConnected {
					w.t.Logf("agent %s not connected yet", agent.Name)
					return false
				}
			}
		}
		resources = workspace.LatestBuild.Resources
		if w.resourcesMatcher == nil {
			return true
		}
		return w.resourcesMatcher(resources)
	}, testutil.WaitLong, testutil.IntervalMedium)
	w.t.Logf("got workspace agents (workspace %s)", w.workspaceID)
	return resources
}

// CreateWorkspace creates a workspace for the user and template provided.
// A random name is generated for it.
// To customize the defaults, pass a mutator func.
func CreateWorkspace(t testing.TB, client *wirtualsdk.Client, templateID uuid.UUID, mutators ...func(*wirtualsdk.CreateWorkspaceRequest)) wirtualsdk.Workspace {
	t.Helper()
	req := wirtualsdk.CreateWorkspaceRequest{
		TemplateID:        templateID,
		Name:              RandomUsername(t),
		AutostartSchedule: ptr.Ref("CRON_TZ=US/Central 30 9 * * 1-5"),
		TTLMillis:         ptr.Ref((8 * time.Hour).Milliseconds()),
		AutomaticUpdates:  wirtualsdk.AutomaticUpdatesNever,
	}
	for _, mutator := range mutators {
		mutator(&req)
	}
	workspace, err := client.CreateUserWorkspace(context.Background(), wirtualsdk.Me, req)
	require.NoError(t, err)
	return workspace
}

// TransitionWorkspace is a convenience method for transitioning a workspace from one state to another.
func MustTransitionWorkspace(t testing.TB, client *wirtualsdk.Client, workspaceID uuid.UUID, from, to database.WorkspaceTransition, muts ...func(req *wirtualsdk.CreateWorkspaceBuildRequest)) wirtualsdk.Workspace {
	t.Helper()
	ctx := context.Background()
	workspace, err := client.Workspace(ctx, workspaceID)
	require.NoError(t, err, "unexpected error fetching workspace")
	require.Equal(t, workspace.LatestBuild.Transition, wirtualsdk.WorkspaceTransition(from), "expected workspace state: %s got: %s", from, workspace.LatestBuild.Transition)

	req := wirtualsdk.CreateWorkspaceBuildRequest{
		TemplateVersionID: workspace.LatestBuild.TemplateVersionID,
		Transition:        wirtualsdk.WorkspaceTransition(to),
	}

	for _, mut := range muts {
		mut(&req)
	}

	build, err := client.CreateWorkspaceBuild(ctx, workspace.ID, req)
	require.NoError(t, err, "unexpected error transitioning workspace to %s", to)

	_ = AwaitWorkspaceBuildJobCompleted(t, client, build.ID)

	updated := MustWorkspace(t, client, workspace.ID)
	require.Equal(t, wirtualsdk.WorkspaceTransition(to), updated.LatestBuild.Transition, "expected workspace to be in state %s but got %s", to, updated.LatestBuild.Transition)
	return updated
}

// MustWorkspace is a convenience method for fetching a workspace that should exist.
func MustWorkspace(t testing.TB, client *wirtualsdk.Client, workspaceID uuid.UUID) wirtualsdk.Workspace {
	t.Helper()
	ctx := context.Background()
	ws, err := client.Workspace(ctx, workspaceID)
	if err != nil && strings.Contains(err.Error(), "status code 410") {
		ws, err = client.DeletedWorkspace(ctx, workspaceID)
	}
	require.NoError(t, err, "no workspace found with id %s", workspaceID)
	return ws
}

// RequestExternalAuthCallback makes a request with the proper OAuth2 state cookie
// to the external auth callback endpoint.
func RequestExternalAuthCallback(t testing.TB, providerID string, client *wirtualsdk.Client, opts ...func(*http.Request)) *http.Response {
	client.HTTPClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	state := "somestate"
	oauthURL, err := client.URL.Parse(fmt.Sprintf("/external-auth/%s/callback?code=asd&state=%s", providerID, state))
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(context.Background(), "GET", oauthURL.String(), nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{
		Name:  wirtualsdk.OAuth2StateCookie,
		Value: state,
	})
	req.AddCookie(&http.Cookie{
		Name:  wirtualsdk.SessionTokenCookie,
		Value: client.SessionToken(),
	})
	for _, opt := range opts {
		opt(req)
	}
	res, err := client.HTTPClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = res.Body.Close()
	})
	return res
}

// NewGoogleInstanceIdentity returns a metadata client and ID token validator for faking
// instance authentication for Google Cloud.
// nolint:revive
func NewGoogleInstanceIdentity(t testing.TB, instanceID string, expired bool) (*idtoken.Validator, *metadata.Client) {
	keyID, err := cryptorand.String(12)
	require.NoError(t, err)
	claims := jwt.MapClaims{
		"google": map[string]interface{}{
			"compute_engine": map[string]string{
				"instance_id": instanceID,
			},
		},
	}
	if !expired {
		claims["exp"] = time.Now().AddDate(1, 0, 0).Unix()
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = keyID
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	signedKey, err := token.SignedString(privateKey)
	require.NoError(t, err)

	// Taken from: https://github.com/googleapis/google-api-go-client/blob/4bb729045d611fa77bdbeb971f6a1204ba23161d/idtoken/validate.go#L57-L75
	type jwk struct {
		Kid string `json:"kid"`
		N   string `json:"n"`
		E   string `json:"e"`
	}
	type certResponse struct {
		Keys []jwk `json:"keys"`
	}

	validator, err := idtoken.NewValidator(context.Background(), option.WithHTTPClient(&http.Client{
		Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
			data, err := json.Marshal(certResponse{
				Keys: []jwk{{
					Kid: keyID,
					N:   base64.RawURLEncoding.EncodeToString(privateKey.N.Bytes()),
					E:   base64.RawURLEncoding.EncodeToString(new(big.Int).SetInt64(int64(privateKey.E)).Bytes()),
				}},
			})
			require.NoError(t, err)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(data)),
				Header:     make(http.Header),
			}, nil
		}),
	}))
	require.NoError(t, err)

	return validator, metadata.NewClient(&http.Client{
		Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(signedKey))),
				Header:     make(http.Header),
			}, nil
		}),
	})
}

// NewAWSInstanceIdentity returns a metadata client and ID token validator for faking
// instance authentication for AWS.
func NewAWSInstanceIdentity(t testing.TB, instanceID string) (awsidentity.Certificates, *http.Client) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	document := []byte(`{"instanceId":"` + instanceID + `"}`)
	hashedDocument := sha256.Sum256(document)

	signatureRaw, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hashedDocument[:])
	require.NoError(t, err)
	signature := make([]byte, base64.StdEncoding.EncodedLen(len(signatureRaw)))
	base64.StdEncoding.Encode(signature, signatureRaw)

	certificate, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(2022),
	}, &x509.Certificate{}, &privateKey.PublicKey, privateKey)
	require.NoError(t, err)

	certificatePEM := bytes.Buffer{}
	err = pem.Encode(&certificatePEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certificate,
	})
	require.NoError(t, err)

	return awsidentity.Certificates{
			awsidentity.Other: certificatePEM.String(),
		}, &http.Client{
			Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
				// Only handle metadata server requests.
				if r.URL.Host != "169.254.169.254" {
					return http.DefaultTransport.RoundTrip(r)
				}
				switch r.URL.Path {
				case "/latest/api/token":
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(bytes.NewReader([]byte("faketoken"))),
						Header:     make(http.Header),
					}, nil
				case "/latest/dynamic/instance-identity/signature":
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(bytes.NewReader(signature)),
						Header:     make(http.Header),
					}, nil
				case "/latest/dynamic/instance-identity/document":
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(bytes.NewReader(document)),
						Header:     make(http.Header),
					}, nil
				default:
					panic("unhandled route: " + r.URL.Path)
				}
			}),
		}
}

// NewAzureInstanceIdentity returns a metadata client and ID token validator for faking
// instance authentication for Azure.
func NewAzureInstanceIdentity(t testing.TB, instanceID string) (x509.VerifyOptions, *http.Client) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	rawCertificate, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(2022),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		Subject: pkix.Name{
			CommonName: "metadata.azure.com",
		},
	}, &x509.Certificate{}, &privateKey.PublicKey, privateKey)
	require.NoError(t, err)

	certificate, err := x509.ParseCertificate(rawCertificate)
	require.NoError(t, err)

	signed, err := pkcs7.NewSignedData([]byte(`{"vmId":"` + instanceID + `"}`))
	require.NoError(t, err)
	err = signed.AddSigner(certificate, privateKey, pkcs7.SignerInfoConfig{})
	require.NoError(t, err)
	signatureRaw, err := signed.Finish()
	require.NoError(t, err)
	signature := make([]byte, base64.StdEncoding.EncodedLen(len(signatureRaw)))
	base64.StdEncoding.Encode(signature, signatureRaw)

	payload, err := json.Marshal(agentsdk.AzureInstanceIdentityToken{
		Signature: string(signature),
		Encoding:  "pkcs7",
	})
	require.NoError(t, err)

	certPool := x509.NewCertPool()
	certPool.AddCert(certificate)

	return x509.VerifyOptions{
			Intermediates: certPool,
			Roots:         certPool,
		}, &http.Client{
			Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
				// Only handle metadata server requests.
				if r.URL.Host != "169.254.169.254" {
					return http.DefaultTransport.RoundTrip(r)
				}
				switch r.URL.Path {
				case "/metadata/attested/document":
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(bytes.NewReader(payload)),
						Header:     make(http.Header),
					}, nil
				default:
					panic("unhandled route: " + r.URL.Path)
				}
			}),
		}
}

func RandomUsername(t testing.TB) string {
	suffix, err := cryptorand.String(3)
	require.NoError(t, err)
	suffix = "-" + suffix
	n := strings.ReplaceAll(namesgenerator.GetRandomName(10), "_", "-") + suffix
	if len(n) > 32 {
		n = n[:32-len(suffix)] + suffix
	}
	return n
}

func RandomName(t testing.TB) string {
	var sb strings.Builder
	var err error
	ss := strings.Split(namesgenerator.GetRandomName(10), "_")
	for si, s := range ss {
		for ri, r := range s {
			if ri == 0 {
				_, err = sb.WriteRune(unicode.ToTitle(r))
				require.NoError(t, err)
			} else {
				_, err = sb.WriteRune(r)
				require.NoError(t, err)
			}
		}
		if si < len(ss)-1 {
			_, err = sb.WriteRune(' ')
			require.NoError(t, err)
		}
	}
	return sb.String()
}

// Used to easily create an HTTP transport!
type roundTripper func(req *http.Request) (*http.Response, error)

func (r roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return r(req)
}

type nopcloser struct{}

func (nopcloser) Close() error { return nil }

// SDKError coerces err into an SDK error.
func SDKError(t testing.TB, err error) *wirtualsdk.Error {
	var cerr *wirtualsdk.Error
	require.True(t, errors.As(err, &cerr))
	return cerr
}

func DeploymentValues(t testing.TB, mut ...func(*wirtualsdk.DeploymentValues)) *wirtualsdk.DeploymentValues {
	cfg := &wirtualsdk.DeploymentValues{}
	opts := cfg.Options()
	err := opts.SetDefaults()
	require.NoError(t, err)
	for _, fn := range mut {
		fn(cfg)
	}
	return cfg
}
