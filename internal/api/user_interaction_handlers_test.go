package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"proxyma/internal/agents"
	interactioninstances "proxyma/internal/interaction/instances"
	"proxyma/internal/users"
	"strings"
	"testing"
)

func TestUserInteractionMutationRejectsDisablingReferencedInstance(t *testing.T) {
	server, paths := testInteractionMutationServer(t)
	instanceID := "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8"
	writeTestNtfyInteraction(t, paths.InteractionConfigDir, "owned.json", instanceID, testUser.ID, "Owned ntfy", "")
	referencingAgent := testAgent
	referencingAgent.UserIDs = []string{testUser.ID}
	referencingAgent.InteractionInstanceID = instanceID
	server.setAgentRegistry(agents.NewRegistry([]agents.Agent{referencingAgent}))
	cookie, session := loginSession(t, server)

	rec := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/interactions/"+instanceID, cookie, session.CSRFToken, `{"enabled":false}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "interaction_referenced") {
		t.Fatalf("disable referenced status = %d body=%s", rec.Code, rec.Body.String())
	}
	_, stored, err := interactioninstances.FindNtfy(paths.InteractionConfigDir, instanceID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Enabled {
		t.Fatal("referenced interaction was disabled")
	}
}

func TestUserInteractionsEndpointFiltersByOwnerAndAdminSeesAll(t *testing.T) {
	server, _ := testInteractionMutationServer(t)
	operator := users.User{
		ID:         "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5",
		Username:   "operator",
		Name:       "Operator",
		Admin:      false,
		PasswdHash: testUser.PasswdHash,
	}
	operatorCookie, _ := addAuthenticatedTestUser(t, server, operator)
	writeTestNtfyInteraction(t, server.ManagementPaths.InteractionConfigDir, "admin.json", "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8", testUser.ID, "Admin ntfy", "")
	writeTestNtfyInteraction(t, server.ManagementPaths.InteractionConfigDir, "operator.json", "7937dd2d-0b8a-4880-8afa-0ea60fd65a43", operator.ID, "Operator ntfy", "")
	referencingAgent := testAgent
	referencingAgent.UserIDs = []string{operator.ID}
	referencingAgent.InteractionInstanceID = "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8"
	server.setAgentRegistry(agents.NewRegistry([]agents.Agent{referencingAgent}))

	operatorReq := httptest.NewRequest(http.MethodGet, "/v1/user/interactions", nil)
	operatorReq.AddCookie(operatorCookie)
	operatorRec := httptest.NewRecorder()
	server.Routes().ServeHTTP(operatorRec, operatorReq)
	if operatorRec.Code != http.StatusOK {
		t.Fatalf("operator status = %d body=%s", operatorRec.Code, operatorRec.Body.String())
	}
	var operatorResponse UserInteractionsResponse
	if err := json.Unmarshal(operatorRec.Body.Bytes(), &operatorResponse); err != nil {
		t.Fatal(err)
	}
	if len(operatorResponse.Interactions) != 2 {
		t.Fatalf("operator response = %#v", operatorResponse)
	}

	adminCookie := loginCookie(t, server)
	adminReq := httptest.NewRequest(http.MethodGet, "/v1/user/interactions", nil)
	adminReq.AddCookie(adminCookie)
	adminRec := httptest.NewRecorder()
	server.Routes().ServeHTTP(adminRec, adminReq)
	if adminRec.Code != http.StatusOK {
		t.Fatalf("admin status = %d body=%s", adminRec.Code, adminRec.Body.String())
	}
	var adminResponse UserInteractionsResponse
	if err := json.Unmarshal(adminRec.Body.Bytes(), &adminResponse); err != nil {
		t.Fatal(err)
	}
	if len(adminResponse.Interactions) != 2 {
		t.Fatalf("admin response = %#v", adminResponse)
	}
}

func TestUserInteractionMutationsCreateUpdateAndPreserveToken(t *testing.T) {
	server, paths := testInteractionMutationServer(t)
	operator := users.User{
		ID:         "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5",
		Username:   "operator",
		Name:       "Operator",
		Admin:      false,
		PasswdHash: testUser.PasswdHash,
	}
	cookie, csrf := addAuthenticatedTestUser(t, server, operator)

	createRec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/interactions", cookie, csrf, `{
  "type": "ntfy",
  "display_name": "Operator phone",
  "enabled": true,
  "base_url": "https://ntfy.example",
  "request_topic": "operator-requests",
  "response_topic": "operator-responses",
  "token": "secret-token",
  "poll_interval": "2s",
  "poll_timeout": "3s",
  "responded_by": "operator-phone"
}`)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created UserInteractionResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.CreatorUserID != operator.ID || !created.TokenConfigured || strings.Contains(createRec.Body.String(), "secret-token") {
		t.Fatalf("created response = %#v body=%s", created, createRec.Body.String())
	}

	updateRec := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/interactions/"+created.ID, cookie, csrf, `{
  "type": "ntfy",
  "display_name": "Renamed phone",
  "enabled": true,
  "base_url": "https://ntfy.example",
  "request_topic": "renamed-requests",
  "response_topic": "renamed-responses",
  "poll_interval": "4s",
  "poll_timeout": "5s",
  "responded_by": "operator-phone"
}`)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", updateRec.Code, updateRec.Body.String())
	}
	_, stored, err := interactioninstances.FindNtfy(paths.InteractionConfigDir, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Token != "secret-token" || stored.DisplayName != "Renamed phone" || stored.RequestTopic != "renamed-requests" {
		t.Fatalf("stored = %#v", stored)
	}
}

func TestUserInteractionMutationsRejectForeignAndReferencedDeletes(t *testing.T) {
	server, paths := testInteractionMutationServer(t)
	operator := users.User{
		ID:         "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5",
		Username:   "operator",
		Name:       "Operator",
		Admin:      false,
		PasswdHash: testUser.PasswdHash,
	}
	operatorCookie, operatorCSRF := addAuthenticatedTestUser(t, server, operator)
	instanceID := "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8"
	writeTestNtfyInteraction(t, paths.InteractionConfigDir, "admin.json", instanceID, testUser.ID, "Admin ntfy", "")

	foreignUpdate := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/interactions/"+instanceID, operatorCookie, operatorCSRF, `{"type":"ntfy","display_name":"Nope"}`)
	if foreignUpdate.Code != http.StatusForbidden {
		t.Fatalf("foreign update status = %d body=%s", foreignUpdate.Code, foreignUpdate.Body.String())
	}

	referencingAgent := testAgent
	referencingAgent.UserIDs = []string{operator.ID}
	referencingAgent.InteractionInstanceID = instanceID
	server.setAgentRegistry(agents.NewRegistry([]agents.Agent{referencingAgent}))
	referencedUpdate := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/interactions/"+instanceID, operatorCookie, operatorCSRF, `{"type":"ntfy","display_name":"Shared ntfy"}`)
	if referencedUpdate.Code != http.StatusOK {
		t.Fatalf("referenced update status = %d body=%s", referencedUpdate.Code, referencedUpdate.Body.String())
	}
	adminCookie, adminSession := loginSession(t, server)
	deleteRec := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/interactions/"+instanceID, adminCookie, adminSession.CSRFToken, "")
	if deleteRec.Code != http.StatusBadRequest || !strings.Contains(deleteRec.Body.String(), "referenced by agent") {
		t.Fatalf("delete status = %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
}
