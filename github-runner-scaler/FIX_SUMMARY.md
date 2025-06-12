# GitHub Runner Scaler - 404 Error Fix Summary

## 🚨 Problem Identified

The Lambda function was failing with **HTTP 404** errors when trying to get workflow runs:

```
failed to get workflow runs (HTTP 404): {
    "message": "Not Found",
    "documentation_url": "https://docs.github.com/rest",
    "status": "404"
}
```

## 🔍 Root Cause Analysis

The original implementation was using an **invalid GitHub API endpoint**:
- ❌ **Used:** `/orgs/{org}/actions/runs` 
- ✅ **Correct:** `/repos/{owner}/{repo}/actions/runs`

**GitHub API Limitation:** Organization-level workflow run endpoints **do not exist**. Workflow runs are only available at the repository level.

## 🔧 Solution Implemented

### 1. **Fixed API Endpoint Structure**
- Changed from organization-level to repository-level API calls
- Now iterates through repositories to get workflow runs from each

### 2. **Repository Discovery**
- **Auto-discovery:** Gets all repositories in the organization automatically
- **Manual configuration:** Optionally specify specific repositories for better performance

### 3. **Enhanced Data Structure**
Added repository context to workflow runs:
```go
type WorkflowRun struct {
    ID         int         `json:"id"`
    Status     string      `json:"status"`
    Repository *Repository `json:"repository,omitempty"` // NEW
}
```

### 4. **Configuration Options**
New environment variable: `REPOSITORY_NAMES`
```json
["repo1", "repo2", "repo3"]
```

## 📝 Changes Made

### Files Modified:
1. **`ghe_client.go`** - Complete rewrite of workflow API calls
2. **`main.go`** - Added repository configuration support  
3. **`pipeline_monitor.go`** - Removed duplicate function
4. **`DEPLOYMENT_GUIDE.md`** - Updated documentation and troubleshooting

### Key Functions Added:
- `GetRepositoriesInOrganization()` - Gets all org repositories
- `getWorkflowRunsAcrossRepos()` - Collects workflow runs from multiple repos
- `getRepositoryWorkflowRuns()` - Gets workflow runs for single repository

## 🎯 How It Works Now

### Kubernetes CRD Approach vs Lambda Approach

| **Aspect** | **Kubernetes CRD** | **Lambda (Fixed)** |
|------------|--------------------|--------------------|
| **Data Source** | Webhook events + Message queues | Repository polling |
| **API Calls** | Event-driven | Scheduled polling |
| **Scope** | Single repository/org | Multiple repositories |
| **Scalability** | High (event-based) | Medium (polling-based) |

### New Workflow:
1. **Get Repositories** - Fetch all repos in organization (or use configured list)
2. **Poll Each Repository** - Get workflow runs from `/repos/{owner}/{repo}/actions/runs`
3. **Aggregate Results** - Combine all workflow runs with repository context
4. **Scale Runners** - Create runners based on queued workflows

## 🚀 Benefits of the Fix

1. **✅ Eliminates 404 Errors** - Uses correct GitHub API endpoints
2. **🎯 Repository Context** - Know which repo needs runners
3. **⚡ Performance Options** - Can monitor specific repos only
4. **📊 Better Monitoring** - Detailed per-repository metrics
5. **🔄 Kubernetes Alignment** - Similar logic to proven CRD implementation

## 🔧 Configuration Examples

### Monitor All Organization Repositories:
```bash
# No REPOSITORY_NAMES specified
export GITHUB_TOKEN="ghp_..."
export ORGANIZATION_NAME="TelenorSweden"
```

### Monitor Specific Repositories Only:
```bash
export REPOSITORY_NAMES='["app-repo", "api-service", "infrastructure"]'
```

### Mixed Organization Repositories:
```bash
export REPOSITORY_NAMES='["TelenorSweden/app1", "OtherOrg/app2"]'
```

## 🧪 Testing the Fix

### 1. Test Repository Access:
```bash
curl -H "Authorization: token YOUR_TOKEN" \
  https://TelenorSwedenAB.ghe.com/api/v3/repos/TelenorSweden/REPO_NAME/actions/runs
```

### 2. Test Organization Repository List:
```bash
curl -H "Authorization: token YOUR_TOKEN" \
  https://TelenorSwedenAB.ghe.com/api/v3/orgs/TelenorSweden/repos
```

### 3. Monitor Lambda Logs:
```bash
aws logs get-log-events \
  --log-group-name "/aws/lambda/github-runner-scaler" \
  --log-stream-name "LATEST_STREAM_NAME"
```

## 📈 Expected Results

After applying this fix:
- ✅ No more 404 errors
- ✅ Workflow runs detected correctly
- ✅ Runners created for queued jobs
- ✅ Better performance with repository filtering
- ✅ Detailed logging with repository context

The implementation now follows the same logical pattern as the Kubernetes CRD but adapted for Lambda polling instead of webhook events. 