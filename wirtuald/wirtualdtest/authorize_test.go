package wirtualdtest_test

import (
	"context"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac/policy"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
)

func TestAuthzRecorder(t *testing.T) {
	t.Parallel()

	t.Run("Authorize", func(t *testing.T) {
		t.Parallel()

		rec := &wirtualdtest.RecordingAuthorizer{
			Wrapped: &wirtualdtest.FakeAuthorizer{},
		}
		sub := wirtualdtest.RandomRBACSubject()
		pairs := fuzzAuthz(t, sub, rec, 10)
		rec.AssertActor(t, sub, pairs...)
		require.NoError(t, rec.AllAsserted(), "all assertions should have been made")
	})

	t.Run("Authorize2Subjects", func(t *testing.T) {
		t.Parallel()

		rec := &wirtualdtest.RecordingAuthorizer{
			Wrapped: &wirtualdtest.FakeAuthorizer{},
		}
		a := wirtualdtest.RandomRBACSubject()
		aPairs := fuzzAuthz(t, a, rec, 10)

		b := wirtualdtest.RandomRBACSubject()
		bPairs := fuzzAuthz(t, b, rec, 10)

		rec.AssertActor(t, b, bPairs...)
		rec.AssertActor(t, a, aPairs...)
		require.NoError(t, rec.AllAsserted(), "all assertions should have been made")
	})

	t.Run("Authorize&Prepared", func(t *testing.T) {
		t.Parallel()

		rec := &wirtualdtest.RecordingAuthorizer{
			Wrapped: &wirtualdtest.FakeAuthorizer{},
		}
		a := wirtualdtest.RandomRBACSubject()
		aPairs := fuzzAuthz(t, a, rec, 10)

		b := wirtualdtest.RandomRBACSubject()

		act, objTy := wirtualdtest.RandomRBACAction(), wirtualdtest.RandomRBACObject().Type
		prep, _ := rec.Prepare(context.Background(), b, act, objTy)
		bPairs := fuzzAuthzPrep(t, prep, 10, act, objTy)

		rec.AssertActor(t, b, bPairs...)
		rec.AssertActor(t, a, aPairs...)
		require.NoError(t, rec.AllAsserted(), "all assertions should have been made")
	})

	t.Run("AuthorizeOutOfOrder", func(t *testing.T) {
		t.Parallel()

		rec := &wirtualdtest.RecordingAuthorizer{
			Wrapped: &wirtualdtest.FakeAuthorizer{},
		}
		sub := wirtualdtest.RandomRBACSubject()
		pairs := fuzzAuthz(t, sub, rec, 10)
		rand.Shuffle(len(pairs), func(i, j int) {
			pairs[i], pairs[j] = pairs[j], pairs[i]
		})

		rec.AssertOutOfOrder(t, sub, pairs...)
		require.NoError(t, rec.AllAsserted(), "all assertions should have been made")
	})

	t.Run("AllCalls", func(t *testing.T) {
		t.Parallel()

		rec := &wirtualdtest.RecordingAuthorizer{
			Wrapped: &wirtualdtest.FakeAuthorizer{},
		}
		sub := wirtualdtest.RandomRBACSubject()
		calls := rec.AllCalls(&sub)
		pairs := make([]wirtualdtest.ActionObjectPair, 0, len(calls))
		for _, call := range calls {
			pairs = append(pairs, wirtualdtest.ActionObjectPair{
				Action: call.Action,
				Object: call.Object,
			})
		}

		rec.AssertActor(t, sub, pairs...)
		require.NoError(t, rec.AllAsserted(), "all assertions should have been made")
	})
}

// fuzzAuthzPrep has same action and object types for all calls.
func fuzzAuthzPrep(t *testing.T, prep rbac.PreparedAuthorized, n int, action policy.Action, objectType string) []wirtualdtest.ActionObjectPair {
	t.Helper()
	pairs := make([]wirtualdtest.ActionObjectPair, 0, n)

	for i := 0; i < n; i++ {
		obj := wirtualdtest.RandomRBACObject()
		obj.Type = objectType
		p := wirtualdtest.ActionObjectPair{Action: action, Object: obj}
		_ = prep.Authorize(context.Background(), p.Object)
		pairs = append(pairs, p)
	}
	return pairs
}

func fuzzAuthz(t *testing.T, sub rbac.Subject, rec rbac.Authorizer, n int) []wirtualdtest.ActionObjectPair {
	t.Helper()
	pairs := make([]wirtualdtest.ActionObjectPair, 0, n)

	for i := 0; i < n; i++ {
		p := wirtualdtest.ActionObjectPair{Action: wirtualdtest.RandomRBACAction(), Object: wirtualdtest.RandomRBACObject()}
		_ = rec.Authorize(context.Background(), sub, p.Action, p.Object)
		pairs = append(pairs, p)
	}
	return pairs
}
