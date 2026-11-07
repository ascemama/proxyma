package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"proxyma/internal/agents"
	interactioninstances "proxyma/internal/interaction/instances"
	"proxyma/internal/targets"
	"proxyma/internal/users"
	"strings"
	"testing"
)

func TestUserAgentsEndpointAdminSeesAllAgentsAndRedactsAgentJWTSigningKeys(t *testing.T) {
	server := testLoginServer(t, false)
	owned := agents.Agent{
		ID:                 testAgent.ID,
		DisplayName:        "Owned Agent",
		AgentJWTSigningKey: "owned-agent-signing-key",
		Enabled:            true,
		UserIDs:            []string{testUser.ID},
	}
	other := agents.Agent{
		ID:                 "7937dd2d-0b8a-4880-8afa-0ea60fd65a43",
		DisplayName:        "Other Agent",
		AgentJWTSigningKey: "other-agent-signing-key",
		Enabled:            true,
		UserIDs:            []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"},
	}
	shared := agents.Agent{
		ID:                 "8dc60324-b3bb-4d19-9aa6-0e149e4938b8",
		DisplayName:        "Shared Agent",
		AgentJWTSigningKey: "multi-owner-agent-signing-key",
		Enabled:            false,
		UserIDs:            []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5", testUser.ID},
	}
	server.Agents = agents.NewRegistry([]agents.Agent{owned, other, shared})
	cookie := loginCookie(t, server)

	req := httptest.NewRequest(http.MethodGet, "/v1/user/agents", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response UserAgentsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Agents) != 3 || response.Agents[0].ID != other.ID || response.Agents[1].ID != shared.ID || response.Agents[2].ID != owned.ID {
		t.Fatalf("response = %#v", response)
	}
	body := rec.Body.String()
	for _, leaked := range []string{"owned-agent-signing-key", "multi-owner-agent-signing-key", "other-agent-signing-key"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("agent response leaked Agent JWT Signing Key %q: %s", leaked, body)
		}
	}
}

func TestUserAgentsEndpointNonAdminFiltersOwnedAgents(t *testing.T) {
	server := testLoginServer(t, false)
	currentUser, found := server.userRegistry().Find(testUser.ID)
	if !found {
		t.Fatal("missing current user")
	}
	currentUser.Admin = false
	server.setUserRegistry(users.NewRegistry([]users.User{currentUser}))
	owned := agents.Agent{
		ID:                 testAgent.ID,
		DisplayName:        "Owned Agent",
		AgentJWTSigningKey: "owned-agent-signing-key",
		Enabled:            true,
		UserIDs:            []string{testUser.ID},
	}
	other := agents.Agent{
		ID:                 "7937dd2d-0b8a-4880-8afa-0ea60fd65a43",
		DisplayName:        "Other Agent",
		AgentJWTSigningKey: "other-agent-signing-key",
		Enabled:            true,
		UserIDs:            []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"},
	}
	shared := agents.Agent{
		ID:                 "8dc60324-b3bb-4d19-9aa6-0e149e4938b8",
		DisplayName:        "Shared Agent",
		AgentJWTSigningKey: "multi-owner-agent-signing-key",
		Enabled:            false,
		UserIDs:            []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5", testUser.ID},
	}
	server.Agents = agents.NewRegistry([]agents.Agent{owned, other, shared})
	cookie := loginCookie(t, server)

	req := httptest.NewRequest(http.MethodGet, "/v1/user/agents", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response UserAgentsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Agents) != 2 || response.Agents[0].ID != shared.ID || response.Agents[1].ID != owned.ID {
		t.Fatalf("response = %#v", response)
	}
}

func TestUserAgentMutationsCreateUpdateDeleteOwnedAgent(t *testing.T) {
	server, paths := testTargetMutationServer(t, nil)
	cookie, session := loginSession(t, server)

	createBody := `{
		"display_name":"Build Agent",
		"agent_jwt_signing_key":"build-agent-signing-key",
		"enabled":true
	}`
	createRec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/agents", cookie, session.CSRFToken, createBody)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	if strings.Contains(createRec.Body.String(), "build-agent-signing-key") {
		t.Fatalf("create response leaked Agent JWT Signing Key: %s", createRec.Body.String())
	}
	var created UserAgentResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.DisplayName != "Build Agent" || !created.Enabled {
		t.Fatalf("created response = %#v", created)
	}
	stored, err := readStoredAgent(paths, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.AgentJWTSigningKey != "build-agent-signing-key" || len(stored.UserIDs) != 1 || stored.UserIDs[0] != testUser.ID || stored.InteractionInstanceID != "local" {
		t.Fatalf("stored agent = %#v", stored)
	}

	updateBody := `{
		"display_name":"Updated Build Agent",
		"enabled":false
	}`
	updateRec := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/agents/"+created.ID, cookie, session.CSRFToken, updateBody)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", updateRec.Code, updateRec.Body.String())
	}
	updated, err := readStoredAgent(paths, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.DisplayName != "Updated Build Agent" || updated.Enabled {
		t.Fatalf("updated agent metadata = %#v", updated)
	}
	if updated.AgentJWTSigningKey != "build-agent-signing-key" {
		t.Fatalf("update did not preserve Agent JWT Signing Key = %#v", updated)
	}
	if refreshed, found := server.agentRegistry().Find(created.ID); !found || refreshed.DisplayName != "Updated Build Agent" {
		t.Fatalf("refreshed registry agent = %#v found=%t", refreshed, found)
	}

	deleteRec := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/agents/"+created.ID, cookie, session.CSRFToken, ``)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	if _, err := readStoredAgent(paths, created.ID); !os.IsNotExist(err) {
		t.Fatalf("expected deleted agent file, err=%v", err)
	}
	if _, found := server.agentRegistry().Find(created.ID); found {
		t.Fatalf("refreshed registry still contained deleted agent")
	}
}

func TestUserAgentInteractionAssignmentsRequireCurrentUserOwnership(t *testing.T) {
	server, paths := testInteractionMutationServer(t)
	ownedID := "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8"
	foreignID := "7937dd2d-0b8a-4880-8afa-0ea60fd65a43"
	disabledID := "1f8bd68e-bbf2-43ed-a89f-2c3cd741f32a"
	foreignUserID := "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"
	writeTestNtfyInteraction(t, paths.InteractionConfigDir, "owned.json", ownedID, testUser.ID, "Owned ntfy", "")
	writeTestNtfyInteraction(t, paths.InteractionConfigDir, "foreign.json", foreignID, foreignUserID, "Foreign ntfy", "")
	writeTestNtfyInteraction(t, paths.InteractionConfigDir, "disabled.json", disabledID, testUser.ID, "Disabled ntfy", "")
	_, disabled, err := interactioninstances.FindNtfy(paths.InteractionConfigDir, disabledID)
	if err != nil {
		t.Fatal(err)
	}
	disabled.Enabled = false
	if _, err := interactioninstances.UpdateNtfy(paths.InteractionConfigDir, disabledID, disabled); err != nil {
		t.Fatal(err)
	}

	cookie, session := loginSession(t, server)
	create := func(instanceID string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"agent_jwt_signing_key":"assignment-signing-key","interaction_instance_id":%q}`, instanceID)
		return userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/agents", cookie, session.CSRFToken, body)
	}

	ownedRec := create(ownedID)
	if ownedRec.Code != http.StatusCreated {
		t.Fatalf("owned assignment status = %d body=%s", ownedRec.Code, ownedRec.Body.String())
	}
	var created UserAgentResponse
	if err := json.Unmarshal(ownedRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.InteractionInstanceID != ownedID {
		t.Fatalf("created interaction instance = %q", created.InteractionInstanceID)
	}

	for name, instanceID := range map[string]string{
		"foreign":  foreignID,
		"disabled": disabledID,
		"unknown":  "0c0591f0-b77c-45af-af3a-bca6c4ea04ee",
	} {
		t.Run(name, func(t *testing.T) {
			rec := create(instanceID)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestUserAgentInteractionAssignmentPreservesLegacyUntilReplaced(t *testing.T) {
	server, paths := testInteractionMutationServer(t)
	ownedID := "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8"
	foreignID := "7937dd2d-0b8a-4880-8afa-0ea60fd65a43"
	writeTestNtfyInteraction(t, paths.InteractionConfigDir, "owned.json", ownedID, testUser.ID, "Owned ntfy", "")
	writeTestNtfyInteraction(t, paths.InteractionConfigDir, "foreign.json", foreignID, "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5", "Foreign ntfy", "")

	legacy := testAgent
	legacy.UserIDs = []string{testUser.ID}
	legacy.InteractionInstanceID = foreignID
	writeJSONRecord(t, paths.AgentDir, legacy.ID, legacy)
	server.setAgentRegistry(agents.NewRegistry([]agents.Agent{legacy}))
	cookie, session := loginSession(t, server)

	omitted := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/agents/"+legacy.ID, cookie, session.CSRFToken, `{"display_name":"Legacy assignment"}`)
	if omitted.Code != http.StatusOK {
		t.Fatalf("omitted status = %d body=%s", omitted.Code, omitted.Body.String())
	}
	unchanged := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/agents/"+legacy.ID, cookie, session.CSRFToken, fmt.Sprintf(`{"interaction_instance_id":%q}`, foreignID))
	if unchanged.Code != http.StatusOK {
		t.Fatalf("unchanged status = %d body=%s", unchanged.Code, unchanged.Body.String())
	}
	replaced := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/agents/"+legacy.ID, cookie, session.CSRFToken, fmt.Sprintf(`{"interaction_instance_id":%q}`, ownedID))
	if replaced.Code != http.StatusOK {
		t.Fatalf("replacement status = %d body=%s", replaced.Code, replaced.Body.String())
	}
	reselectForeign := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/agents/"+legacy.ID, cookie, session.CSRFToken, fmt.Sprintf(`{"interaction_instance_id":%q}`, foreignID))
	if reselectForeign.Code != http.StatusBadRequest {
		t.Fatalf("foreign reselection status = %d body=%s", reselectForeign.Code, reselectForeign.Body.String())
	}
}

func TestUserAgentMutationsRejectForeignAndReferencedAgent(t *testing.T) {
	foreignID := "7937dd2d-0b8a-4880-8afa-0ea60fd65a43"
	server, paths := testTargetMutationServer(t, nil)
	currentUser, found := server.userRegistry().Find(testUser.ID)
	if !found {
		t.Fatal("missing current user")
	}
	currentUser.Admin = false
	server.setUserRegistry(users.NewRegistry([]users.User{currentUser}))
	foreignAgent := agents.Agent{
		ID:                 foreignID,
		DisplayName:        "Foreign Agent",
		AgentJWTSigningKey: "foreign-agent-signing-key",
		Enabled:            true,
		UserIDs:            []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"},
	}
	writeJSONRecord(t, paths.AgentDir, foreignAgent.ID, foreignAgent)
	server.setAgentRegistry(agents.NewRegistry([]agents.Agent{testAgent, foreignAgent}))

	unauthenticated := httptest.NewRecorder()
	server.Routes().ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodPost, "/v1/user/agents", strings.NewReader(`{}`)))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d body=%s", unauthenticated.Code, unauthenticated.Body.String())
	}

	cookie, session := loginSession(t, server)
	invalid := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/agents", cookie, session.CSRFToken, `{"agent_jwt_signing_key":""}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d body=%s", invalid.Code, invalid.Body.String())
	}
	formerField := "shared" + "_" + "secret"
	formerBody := `{"` + formerField + `":"legacy-signing-key"}`
	former := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/agents", cookie, session.CSRFToken, formerBody)
	if former.Code != http.StatusBadRequest {
		t.Fatalf("former field status = %d body=%s", former.Code, former.Body.String())
	}

	missingCSRF := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/agents", cookie, "", `{"agent_jwt_signing_key":"signing-key"}`)
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing csrf status = %d body=%s", missingCSRF.Code, missingCSRF.Body.String())
	}

	updateForeign := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/agents/"+foreignID, cookie, session.CSRFToken, `{"display_name":"Taken"}`)
	if updateForeign.Code != http.StatusForbidden {
		t.Fatalf("foreign update status = %d body=%s", updateForeign.Code, updateForeign.Body.String())
	}
	deleteForeign := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/agents/"+foreignID, cookie, session.CSRFToken, ``)
	if deleteForeign.Code != http.StatusForbidden {
		t.Fatalf("foreign delete status = %d body=%s", deleteForeign.Code, deleteForeign.Body.String())
	}

	createRec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/agents", cookie, session.CSRFToken, `{"agent_jwt_signing_key":"referenced-signing-key"}`)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create referenced status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created UserAgentResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	writeJSONRecord(t, paths.TargetDir, "d252397d-7bd9-41b1-97c7-245ff04fdd33", targets.Target{
		ID:        "d252397d-7bd9-41b1-97c7-245ff04fdd33",
		Transport: targets.TransportSSH,
		OS:        targets.OSLinux,
		Hostname:  "referenced-agent.example",
		Username:  "fixture-user",
		PolicyID:  "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
		AgentIDs:  []string{created.ID},
		UserIDs:   []string{testUser.ID},
		Auth:      targets.PasswordAuth("target-secret"),
	})
	deleteReferenced := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/agents/"+created.ID, cookie, session.CSRFToken, ``)
	if deleteReferenced.Code != http.StatusBadRequest {
		t.Fatalf("referenced delete status = %d body=%s", deleteReferenced.Code, deleteReferenced.Body.String())
	}
}

func TestUserAgentMutationsAdminCanUpdateAndDeleteForeignAgent(t *testing.T) {
	foreignID := "7937dd2d-0b8a-4880-8afa-0ea60fd65a43"
	server, paths := testTargetMutationServer(t, nil)
	adminUser, found := server.userRegistry().Find(testUser.ID)
	if !found {
		t.Fatal("missing admin user")
	}
	foreignUser := users.User{
		ID:         "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5",
		Username:   "operator",
		Name:       "Operator",
		Admin:      false,
		Status:     users.StatusActive,
		PasswdHash: adminUser.PasswdHash,
	}
	writeJSONRecord(t, paths.UserDir, foreignUser.ID, foreignUser)
	server.setUserRegistry(users.NewRegistry([]users.User{adminUser, foreignUser}))
	foreignAgent := agents.Agent{
		ID:                 foreignID,
		DisplayName:        "Foreign Agent",
		AgentJWTSigningKey: "foreign-agent-signing-key",
		Enabled:            true,
		UserIDs:            []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"},
	}
	writeJSONRecord(t, paths.AgentDir, foreignAgent.ID, foreignAgent)
	server.setAgentRegistry(agents.NewRegistry([]agents.Agent{testAgent, foreignAgent}))
	cookie, session := loginSession(t, server)

	updateForeign := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/agents/"+foreignID, cookie, session.CSRFToken, `{"display_name":"Updated Foreign Agent","enabled":false}`)
	if updateForeign.Code != http.StatusOK {
		t.Fatalf("foreign update status = %d body=%s", updateForeign.Code, updateForeign.Body.String())
	}
	updated, err := readStoredAgent(paths, foreignID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.DisplayName != "Updated Foreign Agent" || updated.Enabled || updated.AgentJWTSigningKey != "foreign-agent-signing-key" {
		t.Fatalf("updated agent = %#v", updated)
	}
	if len(updated.UserIDs) != 1 || updated.UserIDs[0] != "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5" {
		t.Fatalf("updated agent user ids = %#v", updated.UserIDs)
	}

	deleteForeign := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/agents/"+foreignID, cookie, session.CSRFToken, ``)
	if deleteForeign.Code != http.StatusOK {
		t.Fatalf("foreign delete status = %d body=%s", deleteForeign.Code, deleteForeign.Body.String())
	}
	if _, err := readStoredAgent(paths, foreignID); !os.IsNotExist(err) {
		t.Fatalf("expected deleted agent file, err=%v", err)
	}
}
