package git

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const (
	// GeminiReviewCommand is the command string used to identify Gemini review comments
	GeminiReviewCommand = "/gemini review"
)

type PRComment struct {
	ID                  int       `json:"id"`
	Body                string    `json:"body"`
	Path                string    `json:"path"`
	Line                int       `json:"line"`
	OriginalLine        int       `json:"original_line"`
	Author              string    `json:"author"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
	State               string    `json:"state"`
	Type                string    `json:"type"`
	CommitID            string    `json:"commit_id"`
	OriginalCommitID    string    `json:"original_commit_id"`
	Position            *int      `json:"position"`
	OriginalPosition    *int      `json:"original_position"`
	PullRequestReviewID int       `json:"pull_request_review_id"`
	IsOutdated          bool      `json:"is_outdated"`
	IsResolved          bool      `json:"is_resolved"`
	IsGeminiReview      bool      `json:"is_gemini_review"`
	Accepted            bool      `json:"-"`
	// Cached rendered content
	RenderedBody string         `json:"-"`
	PlainBody    string         `json:"-"`
	SplitPieces  []CommentPiece `json:"-"`
	IsSplit      bool           `json:"-"`
}

type CommentPiece struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Accepted bool   `json:"accepted"`
	Original string `json:"original"`
}

type PRReview struct {
	ID          int       `json:"id"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	Author      string    `json:"author"`
	SubmittedAt time.Time `json:"submitted_at"`
	CommitID    string    `json:"commit_id"`
	Comments    []PRComment
}

type PullRequest struct {
	Number      int          `json:"number"`
	Title       string       `json:"title"`
	State       string       `json:"state"`
	HeadRef     string       `json:"headRef"`
	BaseRef     string       `json:"baseRef"`
	URL         string       `json:"url"`
	HeadSHA     string       `json:"headSHA"`
	Comments    []*PRComment // Filtered comments (default view)
	AllComments []*PRComment // All comments including outdated/resolved
	Reviews     []PRReview
}

func GetCurrentPR(workingDir string) (*PullRequest, error) {
	cmd := exec.Command("gh", "pr", "view", "--json", "number,title,state,headRefName,baseRefName,url,headRefOid")
	cmd.Dir = workingDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check for common error cases
		outputStr := string(output)
		if strings.Contains(outputStr, "no pull requests found") || strings.Contains(outputStr, "no open pull requests") {
			return nil, fmt.Errorf("no pull request found for the current branch in %s", workingDir)
		}
		if strings.Contains(outputStr, "authentication") || strings.Contains(outputStr, "gh auth login") {
			return nil, fmt.Errorf("GitHub CLI not authenticated. Run 'gh auth login' first")
		}
		if strings.Contains(outputStr, "not a git repository") {
			return nil, fmt.Errorf("not in a git repository: %s", workingDir)
		}
		// Generic error with output for debugging
		return nil, fmt.Errorf("failed to get current PR from %s (output: %s): %w", workingDir, strings.TrimSpace(outputStr), err)
	}

	var prData struct {
		Number      int    `json:"number"`
		Title       string `json:"title"`
		State       string `json:"state"`
		HeadRefName string `json:"headRefName"`
		BaseRefName string `json:"baseRefName"`
		URL         string `json:"url"`
		HeadRefOid  string `json:"headRefOid"`
	}

	if err := json.Unmarshal(output, &prData); err != nil {
		return nil, fmt.Errorf("failed to parse PR data: %w", err)
	}

	pr := &PullRequest{
		Number:  prData.Number,
		Title:   prData.Title,
		State:   prData.State,
		HeadRef: prData.HeadRefName,
		BaseRef: prData.BaseRefName,
		URL:     prData.URL,
		HeadSHA: prData.HeadRefOid,
	}

	return pr, nil
}

func (pr *PullRequest) FetchComments(workingDir string) error {
	// Always clear existing data to ensure fresh fetch
	pr.Comments = []*PRComment{}
	pr.AllComments = []*PRComment{}
	pr.Reviews = []PRReview{}

	// First fetch resolved status for review threads
	resolvedMap, err := pr.fetchResolvedStatus(workingDir)
	if err != nil {
		// Log but don't fail - resolved status is optional
		fmt.Printf("Warning: Could not fetch resolved status: %v\n", err)
		resolvedMap = make(map[int]bool)
	}

	// Fetch PR reviews first
	if err := pr.fetchReviews(workingDir); err != nil {
		return err
	}

	// Fetch review comments (line-specific comments) with resolved status
	if err := pr.fetchReviewComments(workingDir, resolvedMap); err != nil {
		return err
	}

	// Fetch general issue comments
	if err := pr.fetchIssueComments(workingDir); err != nil {
		return err
	}

	// After fetching all comments, separate filtered from all
	filteredComments := make([]*PRComment, 0, len(pr.AllComments))
	for _, comment := range pr.AllComments {
		// Filter out outdated, resolved, and gemini review comments
		if !comment.IsOutdated && !comment.IsResolved && !comment.IsGeminiReview {
			filteredComments = append(filteredComments, comment)
		}
	}
	pr.Comments = filteredComments

	return nil
}

// paginatedGraphQLQuery executes a GraphQL query with pagination support
func paginatedGraphQLQuery(workingDir string, buildQuery func(cursor *string) string, processResponse func([]byte) (hasNextPage bool, endCursor string, err error)) error {
	var cursor *string
	hasNextPage := true
	
	for hasNextPage {
		query := buildQuery(cursor)
		
		// Execute GraphQL query
		cmd := exec.Command("gh", "api", "graphql", "-f", fmt.Sprintf("query=%s", query))
		cmd.Dir = workingDir
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to execute GraphQL query (output: %s): %w", string(output), err)
		}
		
		// Process the response
		nextPage, endCursor, err := processResponse(output)
		if err != nil {
			return err
		}
		
		// Update pagination state
		hasNextPage = nextPage
		if hasNextPage && endCursor != "" {
			cursor = &endCursor
		} else {
			hasNextPage = false
		}
	}
	
	return nil
}

func (pr *PullRequest) fetchResolvedStatus(workingDir string) (map[int]bool, error) {
	// Get repository info first
	repoCmd := exec.Command("gh", "repo", "view", "--json", "owner,name")
	repoCmd.Dir = workingDir
	repoOutput, err := repoCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get repository info: %w", err)
	}

	var repoInfo struct {
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name string `json:"name"`
	}

	if err := json.Unmarshal(repoOutput, &repoInfo); err != nil {
		return nil, fmt.Errorf("failed to parse repository info: %w", err)
	}

	// Build map of comment ID to resolved status
	resolvedMap := make(map[int]bool)
	pageSize := 100

	// Build query function
	buildQuery := func(cursor *string) string {
		var afterClause string
		if cursor != nil {
			afterClause = fmt.Sprintf(`, after: "%s"`, *cursor)
		}
		
		return fmt.Sprintf(`
{
  repository(owner: "%s", name: "%s") {
    pullRequest(number: %d) {
      reviewThreads(first: %d%s) {
        pageInfo {
          hasNextPage
          endCursor
        }
        nodes {
          id
          isResolved
          comments(first: 1) {
            nodes {
              databaseId
            }
          }
        }
      }
    }
  }
}`, repoInfo.Owner.Login, repoInfo.Name, pr.Number, pageSize, afterClause)
	}

	// Process response function
	processResponse := func(output []byte) (bool, string, error) {
		var response struct {
			Data struct {
				Repository struct {
					PullRequest struct {
						ReviewThreads struct {
							PageInfo struct {
								HasNextPage bool   `json:"hasNextPage"`
								EndCursor   string `json:"endCursor"`
							} `json:"pageInfo"`
							Nodes []struct {
								ID         string `json:"id"`
								IsResolved bool   `json:"isResolved"`
								Comments   struct {
									Nodes []struct {
										DatabaseID int `json:"databaseId"`
									} `json:"nodes"`
								} `json:"comments"`
							} `json:"nodes"`
						} `json:"reviewThreads"`
					} `json:"pullRequest"`
				} `json:"repository"`
			} `json:"data"`
		}

		if err := json.Unmarshal(output, &response); err != nil {
			return false, "", fmt.Errorf("failed to parse resolved status response: %w", err)
		}

		// Add comment IDs from this page to the map
		for _, thread := range response.Data.Repository.PullRequest.ReviewThreads.Nodes {
			if len(thread.Comments.Nodes) > 0 {
				commentID := thread.Comments.Nodes[0].DatabaseID
				resolvedMap[commentID] = thread.IsResolved
			}
		}

		pageInfo := response.Data.Repository.PullRequest.ReviewThreads.PageInfo
		return pageInfo.HasNextPage, pageInfo.EndCursor, nil
	}

	// Execute paginated query
	if err := paginatedGraphQLQuery(workingDir, buildQuery, processResponse); err != nil {
		return nil, fmt.Errorf("failed to fetch resolved status: %w", err)
	}

	return resolvedMap, nil
}

func (pr *PullRequest) fetchReviews(workingDir string) error {
	cmd := exec.Command("gh", "api", fmt.Sprintf("repos/{owner}/{repo}/pulls/%d/reviews", pr.Number))
	cmd.Dir = workingDir
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to fetch reviews: %w", err)
	}

	var reviews []struct {
		ID    int    `json:"id"`
		Body  string `json:"body"`
		State string `json:"state"`
		User  struct {
			Login string `json:"login"`
		} `json:"user"`
		SubmittedAt string `json:"submitted_at"`
		CommitID    string `json:"commit_id"`
	}

	if err := json.Unmarshal(output, &reviews); err != nil {
		return fmt.Errorf("failed to parse reviews: %w", err)
	}

	for _, r := range reviews {
		// Skip empty review bodies and dismissed reviews
		if strings.TrimSpace(r.Body) == "" || r.State == "DISMISSED" {
			continue
		}

		submittedAt, err := time.Parse(time.RFC3339, r.SubmittedAt)
		if err != nil {
			return fmt.Errorf("failed to parse submittedAt for review ID %d: %w", r.ID, err)
		}

		// Check if review is outdated (not from the current head commit)
		isOutdated := r.CommitID != pr.HeadSHA

		review := PRReview{
			ID:          r.ID,
			Body:        r.Body,
			State:       r.State,
			Author:      r.User.Login,
			SubmittedAt: submittedAt,
			CommitID:    r.CommitID,
		}

		// Convert review to comment format
		reviewComment := &PRComment{
			ID:                  r.ID,
			Body:                r.Body,
			Author:              r.User.Login,
			CreatedAt:           submittedAt,
			UpdatedAt:           submittedAt,
			State:               strings.ToLower(r.State),
			Type:                "review",
			CommitID:            r.CommitID,
			PullRequestReviewID: r.ID,
			IsOutdated:          isOutdated,
			IsResolved:          r.State == "DISMISSED",
			IsGeminiReview:      strings.Contains(r.Body, GeminiReviewCommand),
			Accepted:            false,
		}
		pr.AllComments = append(pr.AllComments, reviewComment)

		pr.Reviews = append(pr.Reviews, review)
	}

	return nil
}

func (pr *PullRequest) fetchReviewComments(workingDir string, resolvedMap map[int]bool) error {
	cmd := exec.Command("gh", "api", fmt.Sprintf("repos/{owner}/{repo}/pulls/%d/comments", pr.Number))
	cmd.Dir = workingDir
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to fetch review comments: %w", err)
	}

	var reviewComments []struct {
		ID               int    `json:"id"`
		Body             string `json:"body"`
		Path             string `json:"path"`
		Line             *int   `json:"line"`
		OriginalLine     int    `json:"original_line"`
		Position         *int   `json:"position"`
		OriginalPosition *int   `json:"original_position"`
		User             struct {
			Login string `json:"login"`
		} `json:"user"`
		CreatedAt           string `json:"created_at"`
		UpdatedAt           string `json:"updated_at"`
		CommitID            string `json:"commit_id"`
		OriginalCommitID    string `json:"original_commit_id"`
		PullRequestReviewID int    `json:"pull_request_review_id"`
	}

	if err := json.Unmarshal(output, &reviewComments); err != nil {
		return fmt.Errorf("failed to parse review comments: %w", err)
	}

	for _, rc := range reviewComments {
		// Skip empty comments
		if strings.TrimSpace(rc.Body) == "" {
			continue
		}

		createdAt, err := time.Parse(time.RFC3339, rc.CreatedAt)
		if err != nil {
			return fmt.Errorf("failed to parse createdAt for review comment ID %d: %w", rc.ID, err)
		}
		updatedAt, err := time.Parse(time.RFC3339, rc.UpdatedAt)
		if err != nil {
			return fmt.Errorf("failed to parse updatedAt for review comment ID %d: %w", rc.ID, err)
		}

		// Check if comment is outdated
		isOutdated := rc.Position == nil || rc.CommitID != pr.HeadSHA

		// Check if comment is resolved using the resolved map
		isResolved := resolvedMap[rc.ID]

		line := 0
		if rc.Line != nil {
			line = *rc.Line
		}

		comment := &PRComment{
			ID:                  rc.ID,
			Body:                rc.Body,
			Path:                rc.Path,
			Line:                line,
			OriginalLine:        rc.OriginalLine,
			Author:              rc.User.Login,
			CreatedAt:           createdAt,
			UpdatedAt:           updatedAt,
			State:               "pending",
			Type:                "review_comment",
			CommitID:            rc.CommitID,
			OriginalCommitID:    rc.OriginalCommitID,
			Position:            rc.Position,
			OriginalPosition:    rc.OriginalPosition,
			PullRequestReviewID: rc.PullRequestReviewID,
			IsOutdated:          isOutdated,
			IsResolved:          isResolved,
			IsGeminiReview:      strings.Contains(rc.Body, GeminiReviewCommand),
			Accepted:            false,
		}
		pr.AllComments = append(pr.AllComments, comment)
	}

	return nil
}

func (pr *PullRequest) fetchIssueComments(workingDir string) error {
	cmd := exec.Command("gh", "api", fmt.Sprintf("repos/{owner}/{repo}/issues/%d/comments", pr.Number))
	cmd.Dir = workingDir
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to fetch issue comments: %w", err)
	}

	var issueComments []struct {
		ID   int    `json:"id"`
		Body string `json:"body"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
	}

	if err := json.Unmarshal(output, &issueComments); err != nil {
		return fmt.Errorf("failed to parse issue comments: %w", err)
	}

	for _, ic := range issueComments {
		// Skip empty comments
		if strings.TrimSpace(ic.Body) == "" {
			continue
		}

		createdAt, err := time.Parse(time.RFC3339, ic.CreatedAt)
		if err != nil {
			return fmt.Errorf("failed to parse createdAt for issue comment ID %d: %w", ic.ID, err)
		}
		updatedAt, err := time.Parse(time.RFC3339, ic.UpdatedAt)
		if err != nil {
			return fmt.Errorf("failed to parse updatedAt for issue comment ID %d: %w", ic.ID, err)
		}

		comment := &PRComment{
			ID:             ic.ID,
			Body:           ic.Body,
			Author:         ic.User.Login,
			CreatedAt:      createdAt,
			UpdatedAt:      updatedAt,
			State:          "pending",
			Type:           "issue_comment",
			IsOutdated:     false, // Issue comments are never outdated
			IsResolved:     false,
			IsGeminiReview: strings.Contains(ic.Body, GeminiReviewCommand),
			Accepted:       false,
		}
		pr.AllComments = append(pr.AllComments, comment)
	}

	return nil
}

func (pr *PullRequest) GetAcceptedComments() []*PRComment {
	accepted := []*PRComment{}
	// Check both filtered and all comments since accepted state can be set on either
	for _, comment := range pr.AllComments {
		if comment.IsSplit {
			// For split comments, only include if at least one piece is accepted
			hasAcceptedPiece := false
			for _, piece := range comment.SplitPieces {
				if piece.Accepted {
					hasAcceptedPiece = true
					break
				}
			}
			if hasAcceptedPiece {
				accepted = append(accepted, comment)
			}
		} else if comment.Accepted {
			accepted = append(accepted, comment)
		}
	}
	return accepted
}

// PreprocessComments pre-renders markdown for all comments
func (pr *PullRequest) PreprocessComments() {
	for i := range pr.Comments {
		// Store plain body for previews
		pr.Comments[i].PlainBody = stripMarkdownSimple(pr.Comments[i].Body)
		// Use the formatted body as rendered body (no heavy markdown processing)
		pr.Comments[i].RenderedBody = pr.Comments[i].Body
	}
}

// stripMarkdownSimple provides a fast, simple markdown stripping
func stripMarkdownSimple(content string) string {
	// Remove code blocks
	content = regexp.MustCompile("```[\\s\\S]*?```").ReplaceAllString(content, "")

	// Remove inline code
	content = strings.ReplaceAll(content, "`", "")

	// Remove bold and italic markers
	content = regexp.MustCompile(`\*\*([^*]+)\*\*`).ReplaceAllString(content, "$1")
	content = regexp.MustCompile(`__([^_]+)__`).ReplaceAllString(content, "$1")
	content = regexp.MustCompile(`\*([^*]+)\*`).ReplaceAllString(content, "$1")
	content = regexp.MustCompile(`_([^_]+)_`).ReplaceAllString(content, "$1")

	// Remove headers
	content = regexp.MustCompile(`(?m)^#{1,6}\s+`).ReplaceAllString(content, "")

	// Remove links but keep text
	content = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`).ReplaceAllString(content, "$1")

	// Clean up extra whitespace
	content = regexp.MustCompile(`\n{3,}`).ReplaceAllString(content, "\n\n")
	content = strings.TrimSpace(content)

	return content
}

// getCommentStats is a private helper that calculates stats for a given slice of comments
func getCommentStats(comments []*PRComment) (total, reviews, reviewComments, issueComments, outdated, resolved, geminiReviews int) {
	for _, comment := range comments {
		total++
		if comment.IsOutdated {
			outdated++
		}
		if comment.IsResolved {
			resolved++
		}
		if comment.IsGeminiReview {
			geminiReviews++
		}
		switch comment.Type {
		case "review":
			reviews++
		case "review_comment":
			reviewComments++
		case "issue_comment":
			issueComments++
		}
	}
	return
}

// GetCommentStats returns statistics about the filtered comments
func (pr *PullRequest) GetCommentStats() (total, reviews, reviewComments, issueComments, outdated, resolved int) {
	total, reviews, reviewComments, issueComments, outdated, resolved, _ = getCommentStats(pr.Comments)
	return
}

// GetStatsForAllComments returns statistics about all comments (including outdated/resolved)
func (pr *PullRequest) GetStatsForAllComments() (total, reviews, reviewComments, issueComments, outdated, resolved int) {
	total, reviews, reviewComments, issueComments, outdated, resolved, _ = getCommentStats(pr.AllComments)
	return
}

// GetStatsForAllCommentsWithGemini returns statistics about all comments including gemini review count
func (pr *PullRequest) GetStatsForAllCommentsWithGemini() (total, reviews, reviewComments, issueComments, outdated, resolved, geminiReviews int) {
	return getCommentStats(pr.AllComments)
}

// GetAllCommentStats returns statistics about all comments including filtered ones
func (pr *PullRequest) GetAllCommentStats(workingDir string) (total, shown, outdated, resolved int, err error) {
	// We need to refetch to get complete stats including filtered comments
	resolvedMap, err := pr.fetchResolvedStatus(workingDir)
	if err != nil {
		resolvedMap = make(map[int]bool)
	}

	// Count all review comments including filtered ones
	cmd := exec.Command("gh", "api", fmt.Sprintf("repos/{owner}/{repo}/pulls/%d/comments", pr.Number))
	cmd.Dir = workingDir
	output, err := cmd.Output()
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("failed to fetch all comments for stats: %w", err)
	}

	var reviewComments []struct {
		ID       int    `json:"id"`
		Position *int   `json:"position"`
		CommitID string `json:"commit_id"`
	}

	if err := json.Unmarshal(output, &reviewComments); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("failed to parse all comments for stats: %w", err)
	}

	for _, rc := range reviewComments {
		total++
		isOutdated := rc.Position == nil || rc.CommitID != pr.HeadSHA
		isResolved := resolvedMap[rc.ID]

		if isOutdated {
			outdated++
		}
		if isResolved {
			resolved++
		}
		if !isOutdated && !isResolved {
			shown++
		}
	}

	// Add issue comments (they're never filtered)
	cmdIssue := exec.Command("gh", "api", fmt.Sprintf("repos/{owner}/{repo}/issues/%d/comments", pr.Number))
	cmdIssue.Dir = workingDir
	issueOutput, err := cmdIssue.Output()
	if err != nil {
		return total, shown, outdated, resolved, nil // Return what we have
	}

	var issueComments []struct {
		ID int `json:"id"`
	}

	if err := json.Unmarshal(issueOutput, &issueComments); err != nil {
		return total, shown, outdated, resolved, nil
	}

	total += len(issueComments)
	shown += len(issueComments)

	return total, shown, outdated, resolved, nil
}

// GetFilteredComments returns comments that are not outdated or resolved
func (pr *PullRequest) GetFilteredComments() []*PRComment {
	filtered := []*PRComment{}
	for _, comment := range pr.Comments {
		if !comment.IsOutdated && !comment.IsResolved {
			filtered = append(filtered, comment)
		}
	}
	return filtered
}

func (comment *PRComment) GetFormattedBody() string {
	lines := strings.Split(comment.Body, "\n")
	formattedLines := []string{}
	for _, line := range lines {
		if len(line) > 80 {
			for i := 0; i < len(line); i += 80 {
				end := i + 80
				if end > len(line) {
					end = len(line)
				}
				formattedLines = append(formattedLines, line[i:end])
			}
		} else {
			formattedLines = append(formattedLines, line)
		}
	}
	return strings.Join(formattedLines, "\n")
}

// SplitIntoPieces splits a comment into logical pieces based on paragraphs and bullet points
func (comment *PRComment) SplitIntoPieces() {
	if comment.IsSplit {
		return // Already split
	}

	pieces := []CommentPiece{}
	content := strings.TrimSpace(comment.Body)

	// Split by double newlines first (paragraphs)
	paragraphs := strings.Split(content, "\n\n")

	for i, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		// Check if this paragraph contains bullet points
		lines := strings.Split(para, "\n")
		if len(lines) > 1 && looksLikeBulletList(lines) {
			// Split bullet points into individual pieces, but preserve non-bullet lines
			var currentPiece strings.Builder
			pieceIndex := 0

			for _, line := range lines {
				trimmedLine := strings.TrimSpace(line)
				isBullet := trimmedLine != "" && (strings.HasPrefix(trimmedLine, "- ") ||
					strings.HasPrefix(trimmedLine, "* ") ||
					strings.HasPrefix(trimmedLine, "• ") ||
					matchesNumberedList(trimmedLine))

				if isBullet {
					// If we have accumulated non-bullet content, save it as a piece
					if currentPiece.Len() > 0 {
						pieces = append(pieces, CommentPiece{
							ID:       fmt.Sprintf("%d_%d_%d", comment.ID, i, pieceIndex),
							Content:  strings.TrimSpace(currentPiece.String()),
							Accepted: comment.Accepted,
							Original: strings.TrimSpace(currentPiece.String()),
						})
						pieceIndex++
						currentPiece.Reset()
					}

					// Add the bullet as its own piece
					pieces = append(pieces, CommentPiece{
						ID:       fmt.Sprintf("%d_%d_%d", comment.ID, i, pieceIndex),
						Content:  line,
						Accepted: comment.Accepted,
						Original: line,
					})
					pieceIndex++
				} else if trimmedLine != "" {
					// Non-bullet line, accumulate it
					if currentPiece.Len() > 0 {
						currentPiece.WriteString("\n")
					}
					currentPiece.WriteString(line)
				}
			}

			// Don't forget any trailing non-bullet content
			if currentPiece.Len() > 0 {
				pieces = append(pieces, CommentPiece{
					ID:       fmt.Sprintf("%d_%d_%d", comment.ID, i, pieceIndex),
					Content:  strings.TrimSpace(currentPiece.String()),
					Accepted: comment.Accepted,
					Original: strings.TrimSpace(currentPiece.String()),
				})
			}
		} else {
			// Keep paragraph as single piece
			pieces = append(pieces, CommentPiece{
				ID:       fmt.Sprintf("%d_%d", comment.ID, i),
				Content:  para,
				Accepted: comment.Accepted, // Inherit parent's accepted state
				Original: para,
			})
		}
	}

	// If no pieces were created, treat the whole comment as one piece
	if len(pieces) == 0 {
		pieces = append(pieces, CommentPiece{
			ID:       fmt.Sprintf("%d_0", comment.ID),
			Content:  content,
			Accepted: comment.Accepted,
			Original: content,
		})
	}

	comment.SplitPieces = pieces
	comment.IsSplit = true
}

// MergePieces merges the split pieces back into the comment body
func (comment *PRComment) MergePieces() {
	if !comment.IsSplit || len(comment.SplitPieces) == 0 {
		return
	}

	var parts []string
	for _, piece := range comment.SplitPieces {
		if piece.Content != "" {
			parts = append(parts, piece.Content)
		}
	}

	comment.Body = strings.Join(parts, "\n\n")
	comment.IsSplit = false
	comment.SplitPieces = nil
}

// GetAcceptedPieces returns only the accepted pieces from a split comment
func (comment *PRComment) GetAcceptedPieces() []CommentPiece {
	if !comment.IsSplit {
		return nil
	}

	accepted := []CommentPiece{}
	for _, piece := range comment.SplitPieces {
		if piece.Accepted {
			accepted = append(accepted, piece)
		}
	}
	return accepted
}

// Helper function to check if lines look like a bullet list
func looksLikeBulletList(lines []string) bool {
	bulletCount := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") ||
			strings.HasPrefix(trimmed, "• ") || matchesNumberedList(trimmed) {
			bulletCount++
		}
	}
	return bulletCount >= 2 // At least 2 bullet points
}

// Helper function to check if a line matches numbered list pattern
func matchesNumberedList(line string) bool {
	// Match patterns like "1. ", "2) ", "10. ", etc.
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}

	// Must have at least one digit.
	if i == 0 {
		return false
	}

	// After digits, must have '.' or ')' followed by a space.
	if i+1 < len(line) && (line[i] == '.' || line[i] == ')') && line[i+1] == ' ' {
		return true
	}

	return false
}

// GetUnresolvedThreads returns all unresolved review thread IDs
func (pr *PullRequest) GetUnresolvedThreads(workingDir string) ([]string, error) {
	// Get repository info first
	repoCmd := exec.Command("gh", "repo", "view", "--json", "owner,name")
	repoCmd.Dir = workingDir
	repoOutput, err := repoCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get repository info: %w", err)
	}

	var repoInfo struct {
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name string `json:"name"`
	}

	if err := json.Unmarshal(repoOutput, &repoInfo); err != nil {
		return nil, fmt.Errorf("failed to parse repository info: %w", err)
	}

	// Collect all unresolved thread IDs
	var unresolvedThreads []string
	pageSize := 100

	// Build query function
	buildQuery := func(cursor *string) string {
		var afterClause string
		if cursor != nil {
			afterClause = fmt.Sprintf(`, after: "%s"`, *cursor)
		}
		
		return fmt.Sprintf(`
{
  repository(owner: "%s", name: "%s") {
    pullRequest(number: %d) {
      reviewThreads(first: %d%s) {
        totalCount
        pageInfo {
          hasNextPage
          endCursor
        }
        nodes {
          id
          isResolved
          comments(first: 1) {
            nodes {
              body
              author {
                login
              }
            }
          }
        }
      }
    }
  }
}`, repoInfo.Owner.Login, repoInfo.Name, pr.Number, pageSize, afterClause)
	}

	// Process response function
	processResponse := func(output []byte) (bool, string, error) {
		var response struct {
			Data struct {
				Repository struct {
					PullRequest struct {
						ReviewThreads struct {
							TotalCount int `json:"totalCount"`
							PageInfo struct {
								HasNextPage bool   `json:"hasNextPage"`
								EndCursor   string `json:"endCursor"`
							} `json:"pageInfo"`
							Nodes []struct {
								ID         string `json:"id"`
								IsResolved bool   `json:"isResolved"`
								Comments struct {
									Nodes []struct {
										Body string `json:"body"`
										Author struct {
											Login string `json:"login"`
										} `json:"author"`
									} `json:"nodes"`
								} `json:"comments"`
							} `json:"nodes"`
						} `json:"reviewThreads"`
					} `json:"pullRequest"`
				} `json:"repository"`
			} `json:"data"`
		}

		if err := json.Unmarshal(output, &response); err != nil {
			return false, "", fmt.Errorf("failed to parse review threads response: %w", err)
		}

		// Collect unresolved thread IDs from this page
		for _, thread := range response.Data.Repository.PullRequest.ReviewThreads.Nodes {
			if !thread.IsResolved {
				unresolvedThreads = append(unresolvedThreads, thread.ID)
			}
		}

		pageInfo := response.Data.Repository.PullRequest.ReviewThreads.PageInfo
		return pageInfo.HasNextPage, pageInfo.EndCursor, nil
	}

	// Execute paginated query
	if err := paginatedGraphQLQuery(workingDir, buildQuery, processResponse); err != nil {
		return nil, fmt.Errorf("failed to fetch review threads: %w", err)
	}
	
	return unresolvedThreads, nil
}

// ResolveThread resolves a specific review thread
func (pr *PullRequest) ResolveThread(workingDir string, threadID string) error {
	// Use GraphQL mutation to resolve the thread
	mutation := `
mutation($threadId: ID!) {
  resolveReviewThread(input: {threadId: $threadId}) {
    thread {
      id
      isResolved
    }
  }
}`

	// Execute GraphQL mutation using proper parameter passing
	cmd := exec.Command("gh", "api", "graphql", "-f", "query="+mutation, "-f", "threadId="+threadID)
	cmd.Dir = workingDir
	output, err := cmd.CombinedOutput() // Use CombinedOutput to get stderr as well
	if err != nil {
		return fmt.Errorf("failed to resolve thread %s (output: %s): %w", threadID, string(output), err)
	}

	// Check if mutation was successful
	var response struct {
		Data struct {
			ResolveReviewThread struct {
				Thread struct {
					ID         string `json:"id"`
					IsResolved bool   `json:"isResolved"`
				} `json:"thread"`
			} `json:"resolveReviewThread"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(output, &response); err != nil {
		return fmt.Errorf("failed to parse resolve thread response: %w", err)
	}

	if len(response.Errors) > 0 {
		return fmt.Errorf("GraphQL error: %s", response.Errors[0].Message)
	}

	if !response.Data.ResolveReviewThread.Thread.IsResolved {
		return fmt.Errorf("thread %s was not resolved successfully", threadID)
	}

	return nil
}
