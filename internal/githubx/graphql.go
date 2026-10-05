package githubx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var (
	// ErrGraphQLPermissionDenied 权限不足（403/FORBIDDEN/INSUFFICIENT_SCOPES），严格禁止回退 REST
	ErrGraphQLPermissionDenied = errors.New("graphql_permission_denied")
	// ErrGraphQLBatchLimitExceeded 单批数量超出 25 限制
	ErrGraphQLBatchLimitExceeded = errors.New("graphql_batch_limit_exceeded")
)

type GraphQLPRBatchResult struct {
	PullRequests []GraphQLPRNode
	PageInfo     GraphQLPageInfo
}

type GraphQLPRNode struct {
	ID         string
	Number     int
	Title      string
	State      string
	IsDraft    bool
	Mergeable  string
	HeadRefOid string
	Contexts   []CheckOrStatusContext
}

type CheckOrStatusContext struct {
	Typename   string // "CheckRun" | "StatusContext"
	ID         string
	Name       string // CheckRun.name 或 StatusContext.context
	Status     string // CheckRun.status
	Conclusion string // CheckRun.conclusion
	State      string // StatusContext.state
	TargetURL  string // CheckRun.detailsUrl 或 StatusContext.targetUrl
}

type GraphQLPageInfo struct {
	HasNextPage bool
	EndCursor   string
}

const prBatchQuery = `query($owner: String!, $repo: String!, $limit: Int!, $cursor: String) {
  repository(owner: $owner, name: $repo) {
    pullRequests(first: $limit, after: $cursor, states: [OPEN], orderBy: {field: UPDATED_AT, direction: DESC}) {
      pageInfo { hasNextPage endCursor }
      nodes {
        id number title state isDraft mergeable headRefOid
        commits(last: 1) {
          nodes {
            commit {
              oid
              statusCheckRollup {
                contexts(first: 50) {
                  pageInfo { hasNextPage endCursor }
                  nodes {
                    __typename
                    ... on CheckRun { id name status conclusion detailsUrl }
                    ... on StatusContext { id context state targetUrl }
                  }
                }
              }
            }
          }
        }
      }
    }
  }
}`

const prContextsPaginationQuery = `query($owner: String!, $repo: String!, $oid: GitObjectID!, $cursor: String!) {
  repository(owner: $owner, name: $repo) {
    object(oid: $oid) {
      ... on Commit {
        statusCheckRollup {
          contexts(first: 50, after: $cursor) {
            pageInfo { hasNextPage endCursor }
            nodes {
              __typename
              ... on CheckRun { id name status conclusion detailsUrl }
              ... on StatusContext { id context state targetUrl }
            }
          }
        }
      }
    }
  }
}`

// FetchPullRequestsBatch 批量拉取 OPEN 状态的 PR 及其 CI Check/Status 上下文。
// limit 严格受限于 <= 25。
func (c *AppClient) FetchPullRequestsBatch(ctx context.Context, token, owner, repo string, limit int, cursor string) (*GraphQLPRBatchResult, error) {
	if limit <= 0 || limit > 25 {
		return nil, fmt.Errorf("%w: limit must be between 1 and 25 (got %d)", ErrGraphQLBatchLimitExceeded, limit)
	}

	vars := map[string]any{
		"owner": owner,
		"repo":  repo,
		"limit": limit,
	}
	if cursor != "" {
		vars["cursor"] = cursor
	}

	var rawResp struct {
		Data struct {
			Repository struct {
				PullRequests struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []struct {
						ID         string `json:"id"`
						Number     int    `json:"number"`
						Title      string `json:"title"`
						State      string `json:"state"`
						IsDraft    bool   `json:"isDraft"`
						Mergeable  string `json:"mergeable"`
						HeadRefOid string `json:"headRefOid"`
						Commits    struct {
							Nodes []struct {
								Commit struct {
									Oid               string `json:"oid"`
									StatusCheckRollup *struct {
										Contexts struct {
											PageInfo struct {
												HasNextPage bool   `json:"hasNextPage"`
												EndCursor   string `json:"endCursor"`
											} `json:"pageInfo"`
											Nodes []map[string]any `json:"nodes"`
										} `json:"contexts"`
									} `json:"statusCheckRollup"`
								} `json:"commit"`
							} `json:"nodes"`
						} `json:"commits"`
					} `json:"nodes"`
				} `json:"pullRequests"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := c.doGraphQL(ctx, token, prBatchQuery, vars, &rawResp); err != nil {
		return nil, err
	}

	for _, e := range rawResp.Errors {
		if strings.Contains(strings.ToUpper(e.Type), "FORBIDDEN") ||
			strings.Contains(strings.ToUpper(e.Message), "FORBIDDEN") ||
			strings.Contains(strings.ToUpper(e.Message), "INSUFFICIENT_SCOPES") ||
			strings.Contains(strings.ToUpper(e.Message), "NOT ACCESSIBLE BY INTEGRATION") {
			return nil, fmt.Errorf("%w: %s", ErrGraphQLPermissionDenied, e.Message)
		}
	}

	result := &GraphQLPRBatchResult{
		PageInfo: GraphQLPageInfo{
			HasNextPage: rawResp.Data.Repository.PullRequests.PageInfo.HasNextPage,
			EndCursor:   rawResp.Data.Repository.PullRequests.PageInfo.EndCursor,
		},
		PullRequests: make([]GraphQLPRNode, 0, len(rawResp.Data.Repository.PullRequests.Nodes)),
	}

	for _, node := range rawResp.Data.Repository.PullRequests.Nodes {
		pr := GraphQLPRNode{
			ID:         node.ID,
			Number:     node.Number,
			Title:      node.Title,
			State:      node.State,
			IsDraft:    node.IsDraft,
			Mergeable:  node.Mergeable,
			HeadRefOid: node.HeadRefOid,
		}

		if len(node.Commits.Nodes) > 0 {
			commit := node.Commits.Nodes[0].Commit
			if commit.StatusCheckRollup != nil {
				contextsList := parseContextNodes(commit.StatusCheckRollup.Contexts.Nodes)
				// 若存在分页，递归拉取剩余 contexts
				if commit.StatusCheckRollup.Contexts.PageInfo.HasNextPage && commit.StatusCheckRollup.Contexts.PageInfo.EndCursor != "" {
					extra, err := c.fetchRemainingPRContexts(ctx, token, owner, repo, commit.Oid, commit.StatusCheckRollup.Contexts.PageInfo.EndCursor)
					if err == nil {
						contextsList = append(contextsList, extra...)
					}
				}
				pr.Contexts = contextsList
			}
		}

		result.PullRequests = append(result.PullRequests, pr)
	}

	return result, nil
}

func (c *AppClient) fetchRemainingPRContexts(ctx context.Context, token, owner, repo, oid, cursor string) ([]CheckOrStatusContext, error) {
	var all []CheckOrStatusContext
	currentCursor := cursor

	for currentCursor != "" {
		vars := map[string]any{
			"owner":  owner,
			"repo":   repo,
			"oid":    oid,
			"cursor": currentCursor,
		}

		var raw struct {
			Data struct {
				Repository struct {
					Object struct {
						StatusCheckRollup *struct {
							Contexts struct {
								PageInfo struct {
									HasNextPage bool   `json:"hasNextPage"`
									EndCursor   string `json:"endCursor"`
								} `json:"pageInfo"`
								Nodes []map[string]any `json:"nodes"`
							} `json:"contexts"`
						} `json:"statusCheckRollup"`
					} `json:"object"`
				} `json:"repository"`
			} `json:"data"`
		}

		if err := c.doGraphQL(ctx, token, prContextsPaginationQuery, vars, &raw); err != nil {
			return all, err
		}

		rollup := raw.Data.Repository.Object.StatusCheckRollup
		if rollup == nil {
			break
		}

		items := parseContextNodes(rollup.Contexts.Nodes)
		all = append(all, items...)

		if rollup.Contexts.PageInfo.HasNextPage {
			currentCursor = rollup.Contexts.PageInfo.EndCursor
		} else {
			break
		}
	}

	return all, nil
}

func parseContextNodes(nodes []map[string]any) []CheckOrStatusContext {
	res := make([]CheckOrStatusContext, 0, len(nodes))
	for _, n := range nodes {
		typename, _ := n["__typename"].(string)
		id, _ := n["id"].(string)

		ctx := CheckOrStatusContext{
			Typename: typename,
			ID:       id,
		}

		switch typename {
		case "CheckRun":
			ctx.Name, _ = n["name"].(string)
			ctx.Status, _ = n["status"].(string)
			ctx.Conclusion, _ = n["conclusion"].(string)
			ctx.TargetURL, _ = n["detailsUrl"].(string)
		case "StatusContext":
			ctx.Name, _ = n["context"].(string)
			ctx.State, _ = n["state"].(string)
			ctx.TargetURL, _ = n["targetUrl"].(string)
		}
		res = append(res, ctx)
	}
	return res
}

func (c *AppClient) doGraphQL(ctx context.Context, token, query string, vars map[string]any, out any) error {
	endpoint := c.BaseURL + "/graphql"
	if strings.HasSuffix(c.BaseURL, "/") {
		endpoint = c.BaseURL + "graphql"
	}

	reqBody := map[string]any{
		"query":     query,
		"variables": vars,
	}
	rawJSON, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(rawJSON))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "Repo-Sentinel/1.0")

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return err
	}

	// 1. 检查限流响应
	if isRateLimited, rlErr := parseRateLimitError(resp, bodyBytes); isRateLimited {
		return rlErr
	}

	// 2. 检查权限不足（严格禁止回退 REST）
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%w: HTTP %d: %s", ErrGraphQLPermissionDenied, resp.StatusCode, string(bodyBytes))
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("github graphql error (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
	}

	if err := json.Unmarshal(bodyBytes, out); err != nil {
		return fmt.Errorf("graphql json unmarshal failed: %w", err)
	}

	return nil
}
