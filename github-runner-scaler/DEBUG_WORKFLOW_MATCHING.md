# 🐛 **Debug Workflow Label Matching Issue**

## 🎯 **Problem**
Lambda shows: `Filtered 0/147 workflows that match configured labels [self-hosted linux x64 lambda-managed]`

But GitHub shows: `Requested labels: self-hosted, linux, x64, lambda-managed`

## 🔧 **What's Fixed in This Version**

### ✅ **1. Enhanced Debugging**
- Added comprehensive logging to see exactly what's happening
- Shows each workflow and job being checked
- Displays the exact labels being compared
- Shows why jobs are skipped or matched

### ✅ **2. Fixed Label Matching Logic**
- **Before:** Incorrectly required job labels to be subset of runner labels
- **After:** Correctly checks if runner has ALL labels that job requires
- **Logic:** Job with `["self-hosted", "linux", "x64", "lambda-managed"]` can run on runner with those same labels

### ✅ **3. Improved Job Status Checking**
- Added support for `"waiting"` status (in addition to `"queued"`)
- Better handling of job lifecycle states

### ✅ **4. Enhanced Label Field Handling**
- Checks both `labels` and `runs_on` fields from GitHub API
- Fallback logic if one field is empty

## 🚀 **Deploy and Test**

### **Step 1: Deploy Updated Lambda**
```bash
cd terraform/
terraform plan
terraform apply
```

### **Step 2: Trigger Lambda Manually**
```bash
# Test the Lambda function
aws lambda invoke \
  --function-name github-runner-scaler \
  --region us-east-1 \
  --payload '{}' \
  response.json

# Check the response
cat response.json
```

### **Step 3: Check CloudWatch Logs**
```bash
# Get latest logs
aws logs describe-log-groups --log-group-name-prefix "/aws/lambda/github-runner-scaler"

# Stream logs in real-time
aws logs tail /aws/lambda/github-runner-scaler --follow
```

## 🔍 **What to Look For in Logs**

### **Expected Debug Output:**
```
🔍 Checking 147 workflows against configured labels [self-hosted linux x64 lambda-managed]
🔄 [1/147] Checking workflow 12345 in TelenorSweden/test-spot-runner (status: queued)
📋 Found 1 jobs for workflow 12345
   🔍 Job 1/1: ID=67890, Status=queued, Labels=[self-hosted linux x64 lambda-managed]
   🏷️  Checking if job labels [self-hosted linux x64 lambda-managed] match configured [self-hosted linux x64 lambda-managed]
   🔍 Checking if runner labels [self-hosted linux x64 lambda-managed] contain all required job labels [self-hosted linux x64 lambda-managed]
   ✅ Runner has required label: self-hosted
   ✅ Runner has required label: linux
   ✅ Runner has required label: x64
   ✅ Runner has required label: lambda-managed
   🎉 Runner has all required labels!
   ✅ Job 67890 matches! Required: [self-hosted linux x64 lambda-managed], Available: [self-hosted linux x64 lambda-managed]
✅ Workflow 12345 added to matching list
🎯 Final result: Filtered 1/147 workflows that match configured labels [self-hosted linux x64 lambda-managed]
```

### **Troubleshooting Issues:**

#### **Issue 1: No Jobs Found**
```
📋 Found 0 jobs for workflow 12345
```
**Cause:** Workflow API call failed or workflow has no jobs
**Solution:** Check GitHub API permissions and workflow status

#### **Issue 2: Wrong Job Status**
```
⏭️  Skipping job 67890 with status: in_progress
```
**Cause:** Job already picked up by another runner
**Solution:** Normal behavior - job is already running

#### **Issue 3: Missing Labels**
```
🔍 Job 1/1: ID=67890, Status=queued, Labels=[]
📌 Job 67890 also has RunsOn field: []
```
**Cause:** API not returning label information
**Solution:** Check GitHub API response format

#### **Issue 4: Label Mismatch**
```
❌ Runner missing required label: lambda-managed
```
**Cause:** Job requires label that runner doesn't have
**Solution:** Update terraform configuration or workflow

## 🛠️ **Potential Fixes**

### **If Still No Matches Found:**

1. **Check Different Job Status Values:**
   ```go
   // Add more status checks if needed
   if job.Status != "queued" && job.Status != "waiting" && job.Status != "pending" {
   ```

2. **Check GitHub API Response Format:**
   Add temporary debug logging to see raw API response:
   ```go
   body, _ := io.ReadAll(resp.Body)
   log.Printf("Raw API response: %s", string(body))
   ```

3. **Verify GitHub API Endpoint:**
   - Ensure API URL is correct for GitHub Enterprise
   - Check authentication token has proper permissions

## 📋 **Next Steps**

1. **Deploy this version** and check CloudWatch logs
2. **Look for the detailed debug output** to see exactly what's happening
3. **Share the logs** if issue persists - the enhanced logging will show exactly where the problem is
4. **Verify your workflow file** has the correct `runs-on` configuration:
   ```yaml
   jobs:
     test:
       runs-on: [self-hosted, linux, x64, lambda-managed]
   ```

## 🎯 **Expected Outcome**

After this fix, you should see:
- ✅ Lambda correctly identifies workflows that match your labels
- ✅ Runners are created only for matching workflows  
- ✅ No more "Invalid BASE64 encoding" errors
- ✅ Detailed logs showing the decision process

The Lambda will only create runners when there are truly matching jobs waiting! 