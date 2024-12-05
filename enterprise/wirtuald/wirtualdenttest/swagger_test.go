package wirtualdenttest_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/enterprise/wirtuald/wirtualdenttest"
	"github.com/coder/coder/v2/wirtuald/wirtualdtest"
)

func TestEnterpriseEndpointsDocumented(t *testing.T) {
	t.Parallel()

	swaggerComments, err := wirtualdtest.ParseSwaggerComments("..", "../../../wirtuald")
	require.NoError(t, err, "can't parse swagger comments")
	require.NotEmpty(t, swaggerComments, "swagger comments must be present")

	//nolint: dogsled
	_, _, api, _ := wirtualdenttest.NewWithAPI(t, nil)
	wirtualdtest.VerifySwaggerDefinitions(t, api.AGPL.APIHandler, swaggerComments)
}
