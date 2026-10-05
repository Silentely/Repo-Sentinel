package githubx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGraphQLBatch_CheckRunAndStatusContextRollup(t *testing.T) {
	respPayload := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"pullRequests": map[string]any{
					"pageInfo": map[string]any{
						"hasNextPage": false,
						"endCursor":   "cursor-1",
					},
					"nodes": []any{
						map[string]any{
							"id":         "PR_1",
							"number":     42,
							"title":      "fix(core): improve concurrency",
							"state":      "OPEN",
							"isDraft":    false,
							"mergeable":  "MERGEABLE",
							"headRefOid": "commit-sha-abc",
							"commits": map[string]any{
								"nodes": []any{
									map[string]any{
										"commit": map[string]any{
											"oid": "commit-sha-abc",
											"statusCheckRollup": map[string]any{
												"contexts": map[string]any{
													"pageInfo": map[string]any{
														"hasNextPage": false,
														"endCursor":   "ctx-end",
													},
													"nodes": []any{
														map[string]any{
															"__typename": "CheckRun",
															"id":         "cr-1",
															"name":       "ci/test",
															"status":     "COMPLETED",
															"conclusion": "SUCCESS",
															"detailsUrl": "https://ci.example.com/1",
														},
														map[string]any{
															"__typename": "StatusContext",
															"id":         "sc-1",
															"context":    "security/snyk",
															"state":      "SUCCESS",
															"targetUrl":  "https://snyk.example.com/1",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var reqBody map[string]any
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		vars := reqBody["variables"].(map[string]any)
		if vars["owner"] != "Silentely" || vars["repo"] != "Repo-Sentinel" {
			t.Errorf("unexpected variables: %+v", vars)
		}
		if limit, ok := vars["limit"].(float64); !ok || int(limit) > 25 {
			t.Errorf("expected limit <= 25, got %v", vars["limit"])
		}
		_ = json.NewEncoder(w).Encode(respPayload)
	}))
	defer srv.Close()

	client := NewAppClient(1234, "")
	client.BaseURL = srv.URL

	ctx := context.Background()
	res, err := client.FetchPullRequestsBatch(ctx, "mock-token", "Silentely", "Repo-Sentinel", 20, "")
	if err != nil {
		t.Fatalf("FetchPullRequestsBatch failed: %v", err)
	}

	if len(res.PullRequests) != 1 {
		t.Fatalf("expected 1 PR, got %d", len(res.PullRequests))
	}
	pr := res.PullRequests[0]
	if pr.Number != 42 || pr.State != "OPEN" || pr.HeadRefOid != "commit-sha-abc" {
		t.Errorf("unexpected PR basic fields: %+v", pr)
	}
	if len(pr.Contexts) != 2 {
		t.Fatalf("expected 2 rollup contexts, got %d", len(pr.Contexts))
	}

	cr := pr.Contexts[0]
	if cr.Typename != "CheckRun" || cr.Name != "ci/test" || cr.Conclusion != "SUCCESS" || cr.TargetURL != "https://ci.example.com/1" {
		t.Errorf("unexpected CheckRun: %+v", cr)
	}

	sc := pr.Contexts[1]
	if sc.Typename != "StatusContext" || sc.Name != "security/snyk" || sc.State != "SUCCESS" || sc.TargetURL != "https://snyk.example.com/1" {
		t.Errorf("unexpected StatusContext: %+v", sc)
	}
}

func TestGraphQLBatch_ForbiddenRejectsFallback(t *testing.T) {
	// 验证核心安全约束：当遇到权限不足（FORBIDDEN / INSUFFICIENT_SCOPES / 403）时，
	// 必须立即返回权限错误，绝对严禁回退/降级为 REST 请求
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": "Resource not accessible by integration",
			"errors": []any{
				map[string]any{
					"type":    "FORBIDDEN",
					"message": "Resource not accessible by integration",
				},
			},
		})
	}))
	defer srv.Close()

	client := NewAppClient(1234, "")
	client.BaseURL = srv.URL

	ctx := context.Background()
	_, err := client.FetchPullRequestsBatch(ctx, "mock-token", "Silentely", "Repo-Sentinel", 20, "")
	if err == nil {
		t.Fatalf("expected permission error, got nil")
	}

	if !errors.Is(err, ErrGraphQLPermissionDenied) {
		t.Errorf("expected ErrGraphQLPermissionDenied, got: %v", err)
	}
}

func TestGraphQLBatch_SecondaryContextPagination(t *testing.T) {
	// 模拟首批返回 hasNextPage = true 的 contexts，测试二次游标分页聚合
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// 主查询：PR 包含首批 1 个 context，且 hasNextPage=true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"repository": map[string]any{
						"pullRequests": map[string]any{
							"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
							"nodes": []any{
								map[string]any{
									"id":         "PR_100",
									"number":     100,
									"title":      "feat: big pr",
									"state":      "OPEN",
									"headRefOid": "sha-heavy-100",
									"commits": map[string]any{
										"nodes": []any{
											map[string]any{
												"commit": map[string]any{
													"oid": "sha-heavy-100",
													"statusCheckRollup": map[string]any{
														"contexts": map[string]any{
															"pageInfo": map[string]any{"hasNextPage": true, "endCursor": "cursor-page-1"},
															"nodes": []any{
																map[string]any{
																	"__typename": "CheckRun",
																	"id":         "cr-1",
																	"name":       "ci/check-1",
																	"conclusion": "SUCCESS",
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			})
			return
		}

		// 二次查询：拉取剩余 contexts
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"object": map[string]any{
						"statusCheckRollup": map[string]any{
							"contexts": map[string]any{
								"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
								"nodes": []any{
									map[string]any{
										"__typename": "CheckRun",
										"id":         "cr-2",
										"name":       "ci/check-2",
										"conclusion": "SUCCESS",
									},
								},
							},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	client := NewAppClient(1234, "")
	client.BaseURL = srv.URL

	ctx := context.Background()
	res, err := client.FetchPullRequestsBatch(ctx, "mock-token", "Silentely", "Repo-Sentinel", 10, "")
	if err != nil {
		t.Fatalf("FetchPullRequestsBatch failed: %v", err)
	}

	if len(res.PullRequests) != 1 {
		t.Fatalf("expected 1 PR, got %d", len(res.PullRequests))
	}

	pr := res.PullRequests[0]
	if len(pr.Contexts) != 2 {
		t.Fatalf("expected 2 aggregated contexts across pages, got %d", len(pr.Contexts))
	}
	if pr.Contexts[0].Name != "ci/check-1" || pr.Contexts[1].Name != "ci/check-2" {
		t.Errorf("contexts mismatch: %+v", pr.Contexts)
	}
}
