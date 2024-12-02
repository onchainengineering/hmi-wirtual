package wirtualdenttest_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
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
