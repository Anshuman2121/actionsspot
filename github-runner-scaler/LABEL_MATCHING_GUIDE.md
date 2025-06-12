# 🏷️ **Runner Label Matching Guide**

## 🎯 **Problem Solved**

**Before:** Lambda created runners for ALL queued workflows, even if they didn't need the specific runner labels you configured.

**After:** Lambda only creates runners for workflows that specifically match your configured `runner_labels`.

## 🔧 **How Label Matching Works**

### **1. Configuration**
Your `terraform.tfvars` defines the runner labels:
```hcl
runner_labels = ["self-hosted", "linux", "x64", "lambda-managed"]
```

### **2. Workflow Job Requirements**
GitHub Action workflows specify runner requirements using `runs-on`:

```yaml
# Example 1: Matches your configuration ✅
jobs:
  build:
    runs-on: [self-hosted, linux, x64, lambda-managed]
    steps:
      - name: Build
        run: echo "This will use your runners!"

# Example 2: Partial match ✅ (if all required labels are available)
jobs:
  test:
    runs-on: [self-hosted, linux]
    steps:
      - name: Test
        run: echo "This will also use your runners!"

# Example 3: Does NOT match ❌
jobs:
  deploy:
    runs-on: [self-hosted, windows]
    steps:
      - name: Deploy
        run: echo "This will NOT trigger runner creation"

# Example 4: Does NOT match ❌
jobs:
  gpu-training:
    runs-on: [self-hosted, linux, gpu]
    steps:
      - name: Train Model
        run: echo "This will NOT trigger runner creation"
```

### **3. Matching Logic**

The Lambda uses smart label matching:

#### **✅ CREATES RUNNERS When:**
- Workflow requires `[self-hosted, linux, x64, lambda-managed]` → **Perfect match**
- Workflow requires `[self-hosted, linux]` → **Subset match** (all required labels available)
- Workflow requires `[self-hosted]` → **Subset match**
- Workflow requires `[]` (empty) → **Default match** (assumes any self-hosted runner)

#### **❌ DOES NOT CREATE RUNNERS When:**
- Workflow requires `[self-hosted, windows]` → **Missing platform**
- Workflow requires `[self-hosted, linux, gpu]` → **Missing hardware label**
- Workflow requires `[self-hosted, macos, arm64]` → **Different platform**
- Workflow requires `[ubuntu-latest]` → **GitHub-hosted runners only**

## 📊 **Enhanced Logging**

The Lambda now provides detailed filtering information:

```log
🔍 Checking for pending pipelines...
🔍 Filtered 2/10 workflows that match configured labels ["self-hosted", "linux", "x64", "lambda-managed"]
📊 Pipeline Status: Total Queued=10, Matching Queued=2, Total Running=3, Matching Running=1, Available Runners=0, Busy Runners=1
🎯 Action: Need to create 2 runners
✅ Created runner 1/2: lambda-runner-1702845789-0 (spot request: sir-abc123)
✅ Created runner 2/2: lambda-runner-1702845789-1 (spot request: sir-def456)
```

## 🛠️ **Configuration Examples**

### **Scenario 1: Generic Linux Runners**
```hcl
runner_labels = ["self-hosted", "linux", "x64"]
```
**Matches workflows requiring:**
- `[self-hosted, linux, x64]`
- `[self-hosted, linux]`
- `[self-hosted]`

### **Scenario 2: Environment-Specific Runners**
```hcl
runner_labels = ["self-hosted", "linux", "x64", "production", "lambda-managed"]
```
**Matches workflows requiring:**
- `[self-hosted, linux, x64, production, lambda-managed]`
- `[self-hosted, linux, production]`
- `[self-hosted, production]`
- etc.

### **Scenario 3: Hardware-Specific Runners**
```hcl
runner_labels = ["self-hosted", "linux", "x64", "gpu", "nvidia-tesla-v100"]
```
**Matches workflows requiring:**
- `[self-hosted, linux, gpu]`
- `[self-hosted, linux, nvidia-tesla-v100]`
- `[self-hosted, gpu]`

## 🔍 **Debugging Workflow Matching**

### **1. Check Current Workflow Requirements**

List active workflows in your repositories:
```bash
# Get queued workflows
curl -H "Authorization: token YOUR_TOKEN" \
  "https://TelenorSwedenAB.ghe.com/api/v3/repos/TelenorSweden/YOUR_REPO/actions/runs?status=queued"

# Get jobs for a specific workflow run
curl -H "Authorization: token YOUR_TOKEN" \
  "https://TelenorSwedenAB.ghe.com/api/v3/repos/TelenorSweden/YOUR_REPO/actions/runs/RUN_ID/jobs"
```

### **2. Test Label Matching Locally**

```bash
# Set environment variables
export GITHUB_TOKEN="ghp_xxxxx"
export RUNNER_LABELS='["self-hosted", "linux", "x64", "lambda-managed"]'

# Run locally to see filtering results
go run . test-filtering
```

### **3. CloudWatch Logs Analysis**

Look for these log patterns:
```log
# Good: Workflows are being filtered
🔍 Filtered 3/15 workflows that match configured labels

# Investigate: No matching workflows
🔍 Filtered 0/10 workflows that match configured labels

# Action: Runners being created only for matching workflows
🎯 Action: Need to create 3 runners
```

## ⚠️ **Common Issues & Solutions**

### **Issue 1: No Runners Created Despite Queued Workflows**
**Cause:** Your workflows require different labels than configured.

**Solution:** Check your workflow files:
```bash
# Find all workflow files
find .github/workflows/ -name "*.yml" -exec grep -l "runs-on" {} \;

# Check runs-on requirements
grep -r "runs-on:" .github/workflows/
```

### **Issue 2: Too Many Runners Created**
**Cause:** Your labels are too generic (e.g., just `["self-hosted"]`).

**Solution:** Make labels more specific:
```hcl
# Too generic - matches everything
runner_labels = ["self-hosted"]

# Better - more specific
runner_labels = ["self-hosted", "linux", "x64", "my-app"]
```

### **Issue 3: Runners Not Picked Up by Workflows**
**Cause:** Mismatch between workflow requirements and runner labels.

**Solution:** Ensure consistency:
```yaml
# In your workflow
jobs:
  build:
    runs-on: [self-hosted, linux, x64, lambda-managed]  # Must match Terraform config
```

```hcl
# In terraform.tfvars
runner_labels = ["self-hosted", "linux", "x64", "lambda-managed"]  # Must match workflow
```

## 🎯 **Best Practices**

### **1. Use Descriptive Labels**
```hcl
# Good: Clear purpose
runner_labels = ["self-hosted", "linux", "x64", "production", "lambda-managed"]

# Better: Very specific
runner_labels = ["self-hosted", "linux", "x64", "myapp-prod", "lambda-managed", "high-memory"]
```

### **2. Label Naming Conventions**
- **Platform:** `linux`, `windows`, `macos`
- **Architecture:** `x64`, `arm64`
- **Environment:** `production`, `staging`, `development`
- **Purpose:** `ci`, `deployment`, `testing`
- **Hardware:** `gpu`, `high-memory`, `high-cpu`
- **Management:** `lambda-managed`, `manual`, `autoscaled`

### **3. Repository Organization**
Group workflows by runner requirements:
```
.github/workflows/
├── ci-linux.yml          # Uses [self-hosted, linux, x64, lambda-managed]
├── ci-windows.yml         # Uses [self-hosted, windows, x64]
├── deploy-production.yml  # Uses [self-hosted, linux, x64, production]
└── gpu-training.yml       # Uses [self-hosted, linux, gpu]
```

### **4. Monitoring & Alerting**
Set up CloudWatch alarms for:
- Zero matching workflows (indicates configuration mismatch)
- High runner creation rate (indicates label misconfiguration)
- Long-running workflows (indicates resource constraints)

## 🔄 **Migration from Old Behavior**

If you were using the old version that created runners for all workflows:

### **1. Audit Current Workflows**
```bash
# Find all workflow runs requiring different labels
./audit-workflow-labels.sh
```

### **2. Update Configuration**
```hcl
# Old: Generic (matches everything)
runner_labels = ["self-hosted"]

# New: Specific (matches only intended workflows)
runner_labels = ["self-hosted", "linux", "x64", "lambda-managed"]
```

### **3. Update Workflows**
Add the management label to your workflows:
```yaml
jobs:
  build:
    runs-on: [self-hosted, linux, x64, lambda-managed]  # Add lambda-managed
```

### **4. Deploy Gradually**
1. Start with a subset of repositories
2. Monitor CloudWatch logs
3. Verify runners are created only for matching workflows
4. Expand to all repositories

## 📋 **Summary**

✅ **Fixed BASE64 encoding issue** - EC2 user data is now properly encoded  
✅ **Added smart label matching** - Only creates runners for compatible workflows  
✅ **Enhanced logging** - Shows filtering results and matching statistics  
✅ **Improved efficiency** - Reduces unnecessary EC2 instance creation  
✅ **Better cost control** - Only pay for runners that are actually needed 