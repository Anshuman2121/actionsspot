package main

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// CRDStyleJobAnalyzer implements the same logic as actions-runner-controller CRD
// for counting queued and in-progress jobs that match runner labels
type CRDStyleJobAnalyzer struct {
	client *GHEClient
	config Config
}

// JobCount represents the analysis result following CRD pattern
type JobCount struct {
	Total      int `json:"total"`
	Queued     int `json:"queued"`
	InProgress int `json:"in_progress"`
	Completed  int `json:"completed"`
	Unknown    int `json:"unknown"`
	
	// Necessary replicas is the core metric used by ARC
	NecessaryReplicas int `json:"necessary_replicas"`
}

// NewCRDStyleJobAnalyzer creates a new analyzer using CRD logic
func NewCRDStyleJobAnalyzer(client *GHEClient, config Config) *CRDStyleJobAnalyzer {
	return &CRDStyleJobAnalyzer{
		client: client,
		config: config,
	}
}

// AnalyzeJobDemand implements the exact logic from actions-runner-controller
// controllers/actions.summerwind.net/autoscaling.go:suggestReplicasByQueuedAndInProgressWorkflowRuns
func (analyzer *CRDStyleJobAnalyzer) AnalyzeJobDemand(ctx context.Context) (*JobCount, error) {
	log.Printf("🎯 Starting CRD-style job demand analysis...")
	
	// Initialize counters like in ARC
	var total, inProgress, queued, completed, unknown int
	
	// Get repositories to process
	repos, err := analyzer.getRepositoriesToProcess(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get repositories: %w", err)
	}
	
	log.Printf("📊 Processing %d repositories for job analysis", len(repos))
	
	// Process each repository (following ARC pattern)
	for _, repo := range repos {
		log.Printf("🔍 Processing repository: %s", repo.FullName)
		
		// Get workflow runs for this repository
		workflowRuns, err := analyzer.client.getRepositoryWorkflowRuns(ctx, repo.Owner.Login, repo.Name, "")
		if err != nil {
			log.Printf("⚠️  Failed to get workflow runs for %s: %v", repo.FullName, err)
			continue
		}
		
		// Process each workflow run
		for _, run := range workflowRuns.WorkflowRuns {
			total++
			
			// Following ARC logic: only process queued and in_progress workflows
			switch run.Status {
			case "completed":
				completed++
				// Don't fetch jobs for completed workflows to minimize API calls
			case "in_progress":
				jobCounts := analyzer.analyzeWorkflowJobs(ctx, repo.Owner.Login, repo.Name, run.ID)
				inProgress += jobCounts.inProgress
				queued += jobCounts.queued
				unknown += jobCounts.unknown
			case "queued":
				jobCounts := analyzer.analyzeWorkflowJobs(ctx, repo.Owner.Login, repo.Name, run.ID)
				inProgress += jobCounts.inProgress
				queued += jobCounts.queued
				unknown += jobCounts.unknown
			default:
				unknown++
			}
		}
	}
	
	// Calculate necessary replicas (the key metric used by ARC)
	necessaryReplicas := queued + inProgress
	
	result := &JobCount{
		Total:             total,
		Queued:            queued,
		InProgress:        inProgress,
		Completed:         completed,
		Unknown:           unknown,
		NecessaryReplicas: necessaryReplicas,
	}
	
	log.Printf("🎯 CRD-style analysis complete: NecessaryReplicas=%d (queued=%d, inProgress=%d, total=%d)", 
		necessaryReplicas, queued, inProgress, total)
	
	return result, nil
}

// jobAnalysisResult represents job counts for a single workflow
type jobAnalysisResult struct {
	queued     int
	inProgress int
	unknown    int
}

// analyzeWorkflowJobs processes jobs for a specific workflow run
// This implements the exact logic from ARC's listWorkflowJobs function
func (analyzer *CRDStyleJobAnalyzer) analyzeWorkflowJobs(ctx context.Context, owner, repo string, runID int) jobAnalysisResult {
	result := jobAnalysisResult{}
	
	// Get jobs for this workflow run
	jobs, err := analyzer.client.GetWorkflowJobs(ctx, owner, repo, runID)
	if err != nil {
		log.Printf("⚠️  Failed to get jobs for workflow %d in %s/%s: %v", runID, owner, repo, err)
		return result
	}
	
	if len(jobs) == 0 {
		log.Printf("🟡 Workflow %d in %s/%s has no jobs - ignoring for scaling", runID, owner, repo)
		return result
	}
	
	log.Printf("📋 Analyzing %d jobs in workflow %d (%s/%s)", len(jobs), runID, owner, repo)
	
	// Create runner labels map for efficient lookup (following ARC pattern)
	runnerLabels := make(map[string]struct{}, len(analyzer.config.RunnerLabels))
	for _, label := range analyzer.config.RunnerLabels {
		runnerLabels[label] = struct{}{}
	}
	
	// Process each job (following ARC's JOB loop)
	JOB: for _, job := range jobs {
		// Check if job has labels (following ARC validation)
		if len(job.Labels) == 0 {
			log.Printf("🟡 Job %d has no labels - skipping (not supported by ARC pattern)", job.ID)
			continue JOB
		}
		
		log.Printf("   🔍 Job %d: status=%s, labels=%v", job.ID, job.Status, job.Labels)
		
		// Check label compatibility (exact ARC logic)
		for _, label := range job.Labels {
			// Skip self-hosted label check (it's implicit)
			if label == "self-hosted" {
				continue
			}
			
			// If runner doesn't have this required label, skip this job
			if _, ok := runnerLabels[label]; !ok {
				log.Printf("   ❌ Job %d requires label '%s' which runner doesn't have - skipping", job.ID, label)
				continue JOB
			}
		}
		
		// Job matches our runner capabilities - count it based on status
		switch job.Status {
		case "completed":
			// Don't count completed jobs (following ARC logic)
			log.Printf("   ⏭️  Job %d completed - not counted", job.ID)
		case "in_progress":
			result.inProgress++
			log.Printf("   🟢 Job %d in_progress - counted", job.ID)
		case "queued":
			result.queued++
			log.Printf("   🟡 Job %d queued - counted", job.ID)
		default:
			result.unknown++
			log.Printf("   ❓ Job %d has unknown status '%s'", job.ID, job.Status)
		}
	}
	
	log.Printf("   📊 Workflow %d results: queued=%d, inProgress=%d, unknown=%d", 
		runID, result.queued, result.inProgress, result.unknown)
	
	return result
}

// getRepositoriesToProcess gets the list of repositories to analyze
func (analyzer *CRDStyleJobAnalyzer) getRepositoriesToProcess(ctx context.Context) ([]Repository, error) {
	// If specific repositories are configured, use them
	if len(analyzer.config.RepositoryNames) > 0 {
		var repos []Repository
		for _, repoName := range analyzer.config.RepositoryNames {
			owner, name := analyzer.config.OrganizationName, repoName
			
			// Handle "owner/repo" format
			if strings.Contains(repoName, "/") {
				parts := strings.Split(repoName, "/")
				if len(parts) == 2 {
					owner, name = parts[0], parts[1]
				}
			}
			
			repos = append(repos, Repository{
				Name:     name,
				FullName: fmt.Sprintf("%s/%s", owner, name),
				Owner:    &Owner{Login: owner},
			})
		}
		return repos, nil
	}
	
	// Otherwise get all repositories in organization (but filter for Actions-enabled)
	allRepos, err := analyzer.client.GetRepositoriesInOrganization(ctx)
	if err != nil {
		return nil, err
	}
	
	// Filter to only include repositories with Actions enabled
	var enabledRepos []Repository
	for _, repo := range allRepos {
		if analyzer.client.IsGitHubActionsEnabled(ctx, repo.Owner.Login, repo.Name) {
			enabledRepos = append(enabledRepos, repo)
		}
	}
	
	log.Printf("📊 Found %d total repositories, %d with Actions enabled", len(allRepos), len(enabledRepos))
	return enabledRepos, nil
} 