# 🔄 Pipeline Monitoring & Runner Creation

This Lambda function monitors your GitHub Enterprise pipelines and automatically creates runners when needed.

## 🎯 **How It Works**

The pipeline monitor follows this process every 60 seconds:

1. **📋 Check Pending Pipelines**: Queries GHE for `queued` workflow runs
2. **🤖 Analyze Current Runners**: Gets all self-hosted runners and their status
3. **🧮 Calculate Need**: Determines if more runners are needed
4. **🚀 Create Runners**: Spins up AWS spot instances when demand > supply
5. **🧹 Cleanup**: Removes offline runners and terminates unused instances

## 📊 **Decision Logic**

```
IF queued_pipelines > 0 AND available_runners == 0:
    CREATE queued_pipelines new runners
    
ELIF queued_pipelines > available_runners:
    CREATE (queued_pipelines - available_runners) new runners
    
ELSE:
    No action needed
```

## 🚀 **Quick Test**

Test your pipeline monitoring:

```bash
export GITHUB_TOKEN="your_personal_access_token"
export EC2_SUBNET_ID="subnet-xxxxxxx"
export EC2_SECURITY_GROUP_ID="sg-xxxxxxx"
export EC2_KEY_PAIR_NAME="your-key-pair"

go run . test
```

## 📋 **Example Output**

```bash
🧪 Testing Pipeline Monitoring
🔗 Testing GHE connectivity...
✅ Connected to GHE. Found 3 runners

📋 Checking for queued pipelines...
✅ Found 2 queued workflows

📝 Queued Workflows:
   - ID: 12345, Status: queued, Branch: main
   - ID: 12346, Status: queued, Branch: feature/new-feature

🤖 Current Runners:
   🟢 runner-1 - online (BUSY)
   🟢 runner-2 - online
   🔴 runner-3 - offline

🎯 Simulation: What would the monitor do?
📈 Would create 1 new runners (queued: 2, available: 1)
🏁 Test completed
```

## 🛠 **Configuration**

| Variable | Description | Default |
|----------|-------------|---------|
| `GITHUB_TOKEN` | Personal access token | Required |
| `GITHUB_ENTERPRISE_URL` | Your GHE URL | `https://TelenorSwedenAB.ghe.com` |
| `ORGANIZATION_NAME` | GitHub org name | `TelenorSweden` |
| `MIN_RUNNERS` | Minimum runners | `0` |
| `MAX_RUNNERS` | Maximum runners | `10` |
| `CLEANUP_OFFLINE_RUNNERS` | Auto-cleanup | `true` |

## 🎭 **Runner Lifecycle**

### 🚀 **Creation**
1. Get registration token from GHE
2. Create spot instance with runner setup
3. Instance auto-configures and registers with GHE
4. Runner becomes available for jobs

### ⚡ **Execution**
1. Runner picks up queued workflow
2. Executes pipeline steps
3. Reports results back to GHE

### 🏁 **Termination**
1. After job completion, runner becomes idle
2. If configured for ephemeral, runner self-terminates
3. Instance automatically shuts down

## 🚨 **Error Scenarios**

### 🔴 **No Queued Pipelines**
```
✅ No additional runners needed
```

### 🔴 **At Max Capacity**
```
⚠️  Cannot create more runners (already at max: 10)
```

### 🔴 **Connection Issues**
```
❌ Failed to connect to GHE: connection timeout
```

## 🎯 **Key Benefits**

- **💰 Cost Optimized**: Uses spot instances (up to 90% savings)
- **🚀 Fast Scaling**: Creates runners in ~2-3 minutes
- **🧹 Auto Cleanup**: Removes offline/unused runners
- **📊 Smart Logic**: Only creates runners when actually needed
- **🔒 Secure**: Uses ephemeral runners for better security

## 📈 **Monitoring**

Check CloudWatch logs for monitoring:

- **Log Group**: `/aws/lambda/github-runner-scaler`
- **Metrics**: Runner creation/termination events
- **Alarms**: Failed runner creation, connection errors

## 🔄 **Integration with Existing CRD**

This Lambda complements your existing Kubernetes CRD:

- **Lambda**: Handles immediate scaling for urgent pipelines
- **CRD**: Manages long-term capacity and resource allocation
- **Both**: Work together for optimal resource utilization 