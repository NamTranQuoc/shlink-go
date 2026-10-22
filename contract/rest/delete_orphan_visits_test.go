package rest

import (
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

// totalVisits and orphanVisits port the private getTotalVisits/getOrphanVisits helpers shared by
// DeleteOrphanVisitsTest and DeleteShortUrlVisitsTest.
func totalVisits(t *testing.T) any {
	t.Helper()
	payload := h.CallAPIWithKey(t, http.MethodGet, "/visits/non-orphan").JSON(t)
	return h.Dig(payload, "visits", "pagination", "totalItems")
}

func orphanVisits(t *testing.T) any {
	t.Helper()
	payload := h.CallAPIWithKey(t, http.MethodGet, "/visits/orphan").JSON(t)
	return h.Dig(payload, "visits", "pagination", "totalItems")
}

func TestDeleteOrphanVisits_deletesOrphanVisitsWithoutAffectingTheRest(t *testing.T) {
	h.Seed(t)
	h.AssertEquals(t, 7, totalVisits(t))
	h.AssertEquals(t, 3, orphanVisits(t))

	resp := h.CallAPIWithKey(t, http.MethodDelete, "/visits/orphan")
	payload := resp.JSON(t)

	h.AssertEquals(t, 200, resp.StatusCode)
	h.AssertEquals(t, 3, payload["deletedVisits"])
	h.AssertEquals(t, 7, totalVisits(t)) // This verifies that regular visits have not been affected
	h.AssertEquals(t, 0, orphanVisits(t))
}

func TestDeleteOrphanVisits_doesNotDeleteOrphanVisitsForRestrictedApiKey(t *testing.T) {
	h.Seed(t)
	h.AssertEquals(t, 7, totalVisits(t))
	h.AssertEquals(t, 3, orphanVisits(t))

	resp := h.CallAPIWithKeyNamed(t, http.MethodDelete, "/visits/orphan", "no_orphans_api_key")
	payload := resp.JSON(t)

	h.AssertEquals(t, 200, resp.StatusCode)
	h.AssertEquals(t, 0, payload["deletedVisits"])
	h.AssertEquals(t, 7, totalVisits(t))  // This verifies that regular visits have not been affected
	h.AssertEquals(t, 3, orphanVisits(t)) // This verifies that all orphan visits still exist
}
