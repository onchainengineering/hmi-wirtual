package backends_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/enterprise/audit"
	"github.com/onchainengineering/hmi-wirtual/enterprise/audit/audittest"
	"github.com/onchainengineering/hmi-wirtual/enterprise/audit/backends"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbmem"
)

func TestPostgresBackend(t *testing.T) {
	t.Parallel()
	t.Run("OK", func(t *testing.T) {
		t.Parallel()

		var (
			ctx, cancel = context.WithCancel(context.Background())
			db          = dbmem.New()
			pgb         = backends.NewPostgres(db, true)
			alog        = audittest.RandomLog()
		)
		defer cancel()

		err := pgb.Export(ctx, alog, audit.BackendDetails{})
		require.NoError(t, err)

		got, err := db.GetAuditLogsOffset(ctx, database.GetAuditLogsOffsetParams{
			OffsetOpt: 0,
			LimitOpt:  1,
		})
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, alog.ID, got[0].AuditLog.ID)
	})
}
