package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"proxyma/internal/agents"
	"proxyma/internal/targets"
	"proxyma/internal/taskflow/policy"
	"proxyma/internal/users"
	"strings"
	"testing"
)

func TestUserPoliciesEndpointNonAdminSeesAllPolicies(t *testing.T) {
	server, _ := testTargetMutationServer(t, nil)
	currentUser, found := server.userRegistry().Find(testUser.ID)
	if !found {
		t.Fatal("missing test user")
	}
	currentUser.Admin = false
	server.setUserRegistry(users.NewRegistry([]users.User{currentUser}))
	cookie, _ := loginSession(t, server)

	rec := userTargetMutationRequest(t, server, http.MethodGet, "/v1/user/policies", cookie, "", ``)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response UserPoliciesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Policies) != 1 || response.Policies[0].ID != "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c" {
		t.Fatalf("policies = %#v", response.Policies)
	}
}

func TestUserPolicyMutationsCreateRejectInvalidAndDisallowEdit(t *testing.T) {
	server, _ := testTargetMutationServer(t, nil)
	cookie, session := loginSession(t, server)

	createBody := `{
		"name":"Runtime allow policy",
		"description":"Created by API test",
		"enabled":true,
		"rules":[{"id":"allow-hostname","effect":"allow","command_template":"hostname","reason":"test"}]
	}`
	createRec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/policies", cookie, session.CSRFToken, createBody)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created UserPolicyResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Name != "Runtime allow policy" || len(created.Rules) != 1 {
		t.Fatalf("created = %#v", created)
	}
	result, err := server.Policies.Evaluate(context.Background(), policy.Request{PolicyID: created.ID, RemoteCommand: "hostname"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != policy.DecisionAllow {
		t.Fatalf("policy decision = %#v", result)
	}

	invalid := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/policies", cookie, session.CSRFToken, `{"name":"Broken","rules":[{"effect":"allow","command_template":"echo {bad}"}]}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d body=%s", invalid.Code, invalid.Body.String())
	}

	edit := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/policies/"+created.ID, cookie, session.CSRFToken, `{"name":"Edit"}`)
	if edit.Code != http.StatusOK {
		t.Fatalf("edit status = %d body=%s", edit.Code, edit.Body.String())
	}
}

func TestUserPolicyImportAndTargetAssignmentAPI(t *testing.T) {
	target := targets.Target{
		ID:       "8dc60324-b3bb-4d19-9aa6-0e149e4938b8",
		Hostname: "policy-api.example",
		Username: "fixture",
		PolicyID: "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
		AgentIDs: []string{testAgent.ID},
		UserIDs:  []string{testUser.ID},
		Auth:     targets.PasswordAuth("secret"),
	}
	server, _ := testTargetMutationServer(t, []targets.Target{target})
	cookie, session := loginSession(t, server)

	create := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/policies", cookie, session.CSRFToken, `{"name":"Imported diagnostics","rules":[]}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.Code, create.Body.String())
	}
	var created UserPolicyResponse
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	importRec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/policies/"+created.ID+"/rule-lists/import", cookie, session.CSRFToken, `{"version":1,"blacklist":[{"command_template":"rm -rf {path}","reason":"destructive"}],"whitelist":[{"command_template":"ping {ipv4}","reason":"reachability"}]}`)
	if importRec.Code != http.StatusOK {
		t.Fatalf("import status = %d body=%s", importRec.Code, importRec.Body.String())
	}
	var imported UserPolicyRuleListImportResponse
	if err := json.Unmarshal(importRec.Body.Bytes(), &imported); err != nil {
		t.Fatal(err)
	}
	if len(imported.Added) != 2 || imported.Policy.BlacklistRuleCount != 1 || imported.Policy.WhitelistRuleCount != 1 {
		t.Fatalf("imported = %#v", imported)
	}

	assignment := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/policies/"+created.ID+"/targets", cookie, session.CSRFToken, `{"target_ids":["8dc60324-b3bb-4d19-9aa6-0e149e4938b8"]}`)
	if assignment.Code != http.StatusOK {
		t.Fatalf("assignment status = %d body=%s", assignment.Code, assignment.Body.String())
	}
	var assigned UserPolicyResponse
	if err := json.Unmarshal(assignment.Body.Bytes(), &assigned); err != nil {
		t.Fatal(err)
	}
	if len(assigned.AssignedTargetIDs) != 1 || assigned.AssignedTargetIDs[0] != target.ID {
		t.Fatalf("assigned = %#v", assigned.AssignedTargetIDs)
	}

	conflict := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/policies/"+created.ID+"/rule-lists/import", cookie, session.CSRFToken, `{"version":1,"blacklist":[{"command_template":"ping {ipv4}"}]}`)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "ping {ipv4}") {
		t.Fatalf("conflict status = %d body=%s", conflict.Code, conflict.Body.String())
	}
}

func TestUserPolicyDeleteRejectsReferencedPolicyAndRefreshesEvaluator(t *testing.T) {
	referencedTarget := targets.Target{
		ID:       "8dc60324-b3bb-4d19-9aa6-0e149e4938b8",
		Hostname: "referenced.example",
		Username: "fixture",
		PolicyID: "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
		AgentIDs: []string{testAgent.ID},
		UserIDs:  []string{testUser.ID},
		Auth:     targets.PasswordAuth("secret"),
	}
	server, _ := testTargetMutationServer(t, []targets.Target{referencedTarget})
	cookie, session := loginSession(t, server)

	referencedDelete := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/policies/6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c", cookie, session.CSRFToken, ``)
	if referencedDelete.Code != http.StatusBadRequest {
		t.Fatalf("referenced delete status = %d body=%s", referencedDelete.Code, referencedDelete.Body.String())
	}

	createBody := `{"name":"Temporary policy","rules":[{"id":"allow-all","effect":"allow","command_template":"hostname"}]}`
	createRec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/policies", cookie, session.CSRFToken, createBody)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created UserPolicyResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	deleteRec := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/policies/"+created.ID, cookie, session.CSRFToken, ``)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	result, err := server.Policies.Evaluate(context.Background(), policy.Request{PolicyID: created.ID, RemoteCommand: "hostname"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != policy.DecisionDeny || !strings.Contains(result.Reason, "unknown policy") {
		t.Fatalf("policy decision after delete = %#v", result)
	}
}

func TestUserMutationsListCreateUpdateDeleteUser(t *testing.T) {
	server, paths := testTargetMutationServer(t, nil)
	cookie, session := loginSession(t, server)

	listRec := userTargetMutationRequest(t, server, http.MethodGet, "/v1/user/users", cookie, "", ``)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	if strings.Contains(listRec.Body.String(), testUser.PasswdHash) || strings.Contains(listRec.Body.String(), "passwd_hash") {
		t.Fatalf("list response leaked password hash: %s", listRec.Body.String())
	}

	createBody := `{
		"username":"ops-user",
		"name":"Operations User",
		"admin":false
	}`
	createRec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/users", cookie, session.CSRFToken, createBody)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	if strings.Contains(createRec.Body.String(), "passwd_hash") {
		t.Fatalf("create response leaked password data: %s", createRec.Body.String())
	}
	var created UserProfileResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Username != "ops-user" || created.Name != "Operations User" || created.Admin || created.Status != users.StatusPendingActivation || created.ActivationURL == "" {
		t.Fatalf("created response = %#v", created)
	}
	stored, err := readStoredUser(paths, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.PasswdHash != "" || stored.Status != users.StatusPendingActivation {
		t.Fatalf("stored pending user = %#v", stored)
	}
	originalHash := stored.PasswdHash

	updateBody := `{
		"username":"ops-user-2",
		"name":"Operations User 2",
		"admin":true
	}`
	updateRec := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/users/"+created.ID, cookie, session.CSRFToken, updateBody)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", updateRec.Code, updateRec.Body.String())
	}
	updated, err := readStoredUser(paths, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Username != "ops-user-2" || updated.Name != "Operations User 2" || !updated.Admin {
		t.Fatalf("updated user metadata = %#v", updated)
	}
	if updated.PasswdHash != originalHash {
		t.Fatalf("blank password update did not preserve hash")
	}
	if refreshed, found := server.userRegistry().Find(created.ID); !found || refreshed.Username != "ops-user-2" {
		t.Fatalf("refreshed registry user = %#v found=%t", refreshed, found)
	}

	deleteRec := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/users/"+created.ID, cookie, session.CSRFToken, ``)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	if _, err := readStoredUser(paths, created.ID); !os.IsNotExist(err) {
		t.Fatalf("expected deleted user file, err=%v", err)
	}
	if _, found := server.userRegistry().Find(created.ID); found {
		t.Fatalf("refreshed registry still contained deleted user")
	}
}

func TestUserMutationsRejectNonAdminAndLockoutRequests(t *testing.T) {
	server, paths := testTargetMutationServer(t, nil)
	adminUser, found := server.userRegistry().Find(testUser.ID)
	if !found {
		t.Fatal("missing admin user")
	}
	nonAdmin := users.User{
		ID:         "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5",
		Username:   "operator",
		Name:       "Operator",
		Admin:      false,
		Status:     users.StatusActive,
		PasswdHash: adminUser.PasswdHash,
	}
	writeJSONRecord(t, paths.UserDir, nonAdmin.ID, nonAdmin)
	server.setUserRegistry(users.NewRegistry([]users.User{adminUser, nonAdmin}))

	nonAdminLogin := loginRequest(t, server, `{"username":"operator","password":"correct-password"}`)
	if nonAdminLogin.Code != http.StatusOK {
		t.Fatalf("non-admin login status = %d body=%s", nonAdminLogin.Code, nonAdminLogin.Body.String())
	}
	nonAdminCookie := requireCookie(t, nonAdminLogin, userSessionCookieName)
	var nonAdminSession UserSessionResponse
	if err := json.Unmarshal(nonAdminLogin.Body.Bytes(), &nonAdminSession); err != nil {
		t.Fatal(err)
	}
	for _, request := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/v1/user/users", ``},
		{http.MethodPost, "/v1/user/users", `{"username":"blocked","name":"Blocked","password":"password"}`},
		{http.MethodPut, "/v1/user/users/" + testUser.ID, `{"username":"demo-admin","name":"Demo Admin","admin":true}`},
		{http.MethodDelete, "/v1/user/users/" + testUser.ID, ``},
	} {
		rec := userTargetMutationRequest(t, server, request.method, request.path, nonAdminCookie, nonAdminSession.CSRFToken, request.body)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s status = %d body=%s", request.method, request.path, rec.Code, rec.Body.String())
		}
	}

	adminCookie, adminSession := loginSession(t, server)
	selfDelete := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/users/"+testUser.ID, adminCookie, adminSession.CSRFToken, ``)
	if selfDelete.Code != http.StatusBadRequest {
		t.Fatalf("self delete status = %d body=%s", selfDelete.Code, selfDelete.Body.String())
	}
	selfDemote := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/users/"+testUser.ID, adminCookie, adminSession.CSRFToken, `{"username":"demo-admin","name":"Demo Admin","admin":false}`)
	if selfDemote.Code != http.StatusBadRequest {
		t.Fatalf("self demote status = %d body=%s", selfDemote.Code, selfDemote.Body.String())
	}
}

func TestUserMutationsRejectReferencedUserDelete(t *testing.T) {
	server, paths := testTargetMutationServer(t, nil)
	cookie, session := loginSession(t, server)
	createRec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/users", cookie, session.CSRFToken, `{
		"username":"owned-agent-user",
		"name":"Owned Agent User",
		"admin":false
	}`)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created UserProfileResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	writeJSONRecord(t, paths.AgentDir, "d252397d-7bd9-41b1-97c7-245ff04fdd33", agents.Agent{
		ID:                 "d252397d-7bd9-41b1-97c7-245ff04fdd33",
		DisplayName:        "Owned User Agent",
		AgentJWTSigningKey: "owned-user-agent-signing-key",
		Enabled:            true,
		UserIDs:            []string{created.ID},
	})
	deleteReferenced := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/users/"+created.ID, cookie, session.CSRFToken, ``)
	if deleteReferenced.Code != http.StatusBadRequest {
		t.Fatalf("referenced delete status = %d body=%s", deleteReferenced.Code, deleteReferenced.Body.String())
	}
}
