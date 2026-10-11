package rpc_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
)

// "Health cannot be set by configuration": no trunk request has anywhere
// to put one.
func TestTrunkRequests_CannotCarryHealth(t *testing.T) {
	for _, m := range []proto.Message{&atsapbxv1.CreateTrunkRequest{}, &atsapbxv1.UpdateTrunkRequest{}} {
		for _, n := range fieldNames(m) {
			assert.NotContains(t, strings.ToLower(n), "health", "%T must not let a caller assert health", m)
		}
	}
}

// The carrier secret is write-only: only the two requests accept it, and
// no response or resource can carry it back. has_secret says whether one
// is stored, and nothing more.
func TestTrunkSecret_IsWriteOnly(t *testing.T) {
	accepts := func(m proto.Message) bool {
		for _, n := range fieldNames(m) {
			if n == "secret" {
				return true
			}
		}
		return false
	}
	assert.True(t, accepts(&atsapbxv1.CreateTrunkRequest{}))
	assert.True(t, accepts(&atsapbxv1.UpdateTrunkRequest{}))

	for _, m := range []proto.Message{
		&atsapbxv1.Trunk{}, &atsapbxv1.CreateTrunkResponse{}, &atsapbxv1.UpdateTrunkResponse{},
		&atsapbxv1.ListTrunksResponse{}, &atsapbxv1.DeleteTrunkResponse{},
		&atsapbxv1.Route{}, &atsapbxv1.CreateRouteResponse{}, &atsapbxv1.ListRoutesResponse{},
	} {
		for _, n := range fieldNames(m) {
			lower := strings.ToLower(n)
			if lower == "has_secret" {
				continue
			}
			assert.NotContains(t, lower, "secret", "%T must not carry the secret", m)
			assert.NotContains(t, lower, "password", "%T", m)
			assert.NotContains(t, lower, "credential", "%T must not expose the credential reference", m)
		}
	}
	assert.Contains(t, fieldNames(&atsapbxv1.Trunk{}), "has_secret")
}
