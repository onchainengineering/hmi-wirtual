package cli_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/cli/clitest"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/license"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
	"github.com/onchainengineering/hmi-wirtual/pty/ptytest"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func TestEnterpriseCreate(t *testing.T) {
	t.Parallel()

	type setupData struct {
		firstResponse wirtualsdk.CreateFirstUserResponse
		second        wirtualsdk.Organization
		owner         *wirtualsdk.Client
		member        *wirtualsdk.Client
	}

	type setupArgs struct {
		firstTemplates  []string
		secondTemplates []string
	}

	// setupMultipleOrganizations creates an extra organization, assigns a member
	// both organizations, and optionally creates templates in each organization.
	setupMultipleOrganizations := func(t *testing.T, args setupArgs) setupData {
		ownerClient, first := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				// This only affects the first org.
				IncludeProvisionerDaemon: true,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureExternalProvisionerDaemons: 1,
					wirtualsdk.FeatureMultipleOrganizations:      1,
				},
			},
		})

		second := wirtualdenttest.CreateOrganization(t, ownerClient, wirtualdenttest.CreateOrganizationOptions{
			IncludeProvisionerDaemon: true,
		})
		member, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, first.OrganizationID, rbac.ScopedRoleOrgMember(second.ID))

		var wg sync.WaitGroup

		createTemplate := func(tplName string, orgID uuid.UUID) {
			version := wirtualdtest.CreateTemplateVersion(t, ownerClient, orgID, nil)
			wg.Add(1)
			go func() {
				wirtualdtest.AwaitTemplateVersionJobCompleted(t, ownerClient, version.ID)
				wg.Done()
			}()

			wirtualdtest.CreateTemplate(t, ownerClient, orgID, version.ID, func(request *wirtualsdk.CreateTemplateRequest) {
				request.Name = tplName
			})
		}

		for _, tplName := range args.firstTemplates {
			createTemplate(tplName, first.OrganizationID)
		}

		for _, tplName := range args.secondTemplates {
			createTemplate(tplName, second.ID)
		}

		wg.Wait()

		return setupData{
			firstResponse: first,
			owner:         ownerClient,
			second:        second,
			member:        member,
		}
	}

	// Test creating a workspace in the second organization with a template
	// name.
	t.Run("CreateMultipleOrganization", func(t *testing.T) {
		t.Parallel()

		const templateName = "secondtemplate"
		setup := setupMultipleOrganizations(t, setupArgs{
			secondTemplates: []string{templateName},
		})
		member := setup.member

		args := []string{
			"create",
			"my-workspace",
			"-y",
			"--template", templateName,
		}
		inv, root := clitest.New(t, args...)
		clitest.SetupConfig(t, member, root)
		_ = ptytest.New(t).Attach(inv)
		err := inv.Run()
		require.NoError(t, err)

		ws, err := member.WorkspaceByOwnerAndName(context.Background(), wirtualsdk.Me, "my-workspace", wirtualsdk.WorkspaceOptions{})
		if assert.NoError(t, err, "expected workspace to be created") {
			assert.Equal(t, ws.TemplateName, templateName)
			assert.Equal(t, ws.OrganizationName, setup.second.Name, "workspace in second organization")
		}
	})

	// If a template name exists in two organizations, the workspace create will
	// fail.
	t.Run("AmbiguousTemplateName", func(t *testing.T) {
		t.Parallel()

		const templateName = "ambiguous"
		setup := setupMultipleOrganizations(t, setupArgs{
			firstTemplates:  []string{templateName},
			secondTemplates: []string{templateName},
		})
		member := setup.member

		args := []string{
			"create",
			"my-workspace",
			"-y",
			"--template", templateName,
		}
		inv, root := clitest.New(t, args...)
		clitest.SetupConfig(t, member, root)
		_ = ptytest.New(t).Attach(inv)
		err := inv.Run()
		require.Error(t, err, "expected error due to ambiguous template name")
		require.ErrorContains(t, err, "multiple templates found")
	})

	// Ambiguous template names are allowed if the organization is specified.
	t.Run("WorkingAmbiguousTemplateName", func(t *testing.T) {
		t.Parallel()

		const templateName = "ambiguous"
		setup := setupMultipleOrganizations(t, setupArgs{
			firstTemplates:  []string{templateName},
			secondTemplates: []string{templateName},
		})
		member := setup.member

		args := []string{
			"create",
			"my-workspace",
			"-y",
			"--template", templateName,
			"--org", setup.second.Name,
		}
		inv, root := clitest.New(t, args...)
		clitest.SetupConfig(t, member, root)
		_ = ptytest.New(t).Attach(inv)
		err := inv.Run()
		require.NoError(t, err)

		ws, err := member.WorkspaceByOwnerAndName(context.Background(), wirtualsdk.Me, "my-workspace", wirtualsdk.WorkspaceOptions{})
		if assert.NoError(t, err, "expected workspace to be created") {
			assert.Equal(t, ws.TemplateName, templateName)
			assert.Equal(t, ws.OrganizationName, setup.second.Name, "workspace in second organization")
		}
	})

	// If an organization is specified, but the template is not in that
	// organization, an error is thrown.
	t.Run("CreateIncorrectOrg", func(t *testing.T) {
		t.Parallel()

		const templateName = "secondtemplate"
		setup := setupMultipleOrganizations(t, setupArgs{
			firstTemplates: []string{templateName},
		})
		member := setup.member

		args := []string{
			"create",
			"my-workspace",
			"-y",
			"--org", setup.second.Name,
			"--template", templateName,
		}
		inv, root := clitest.New(t, args...)
		clitest.SetupConfig(t, member, root)
		_ = ptytest.New(t).Attach(inv)
		err := inv.Run()
		require.Error(t, err)
		// The error message should indicate the flag to fix the issue.
		require.ErrorContains(t, err, fmt.Sprintf("--org=%q", "coder"))
	})
}
