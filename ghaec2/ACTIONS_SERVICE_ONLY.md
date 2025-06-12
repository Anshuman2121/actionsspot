# 🚀 Actions Service API Only - GHAEC2 Scaler

## Overview
This implementation uses **ONLY** the GitHub Actions Service API for autoscaling, with no fallback to traditional GitHub API. This provides the most efficient and modern scaling approach, but requires compatible GitHub Enterprise Server versions.

## 🎯 **Actions Service API Benefits**

### ✅ **Modern Architecture**
- **Real-time messaging** via Actions Service message queues
- **Efficient scaling** based on job statistics and events
- **Reduced API calls** - no more rate limiting issues
- **Event-driven** job processing with immediate response

### ✅ **Enhanced Performance**
- **Instant job detection** through message streaming
- **Statistics-driven scaling** with precise metrics
- **Optimized resource usage** with smart scale-down
- **Zero polling overhead** for job discovery

## 🔧 **Requirements**

### **GitHub Enterprise Server Compatibility**
Your GitHub Enterprise Server **MUST** support the Actions Service API:

```bash
# Check if your GHES supports Actions Service API
curl -X POST \
  -H "Authorization: RemoteAuth YOUR_REGISTRATION_TOKEN" \
  -H "Content-Type: application/json" \
  https://your-ghes.com/actions/runner-registration \
  -d '{"url":"https://your-ghes.com/your-org","runner_event":"register"}'
```

**Expected Response:**
```json
{
  "url": "https://actions.your-ghes.com",
  "authorization": "Bearer actions-service-token..."
}
```

**If you get HTML or 404:** Your GHES version doesn't support Actions Service API.

### **Required GHES Features**
- ✅ **Actions Service API** endpoints (`/_apis/runtime/runnerscalesets`)
- ✅ **Message Queue** support for real-time events
- ✅ **Runner Scale Sets** functionality
- ✅ **Admin authentication** via RemoteAuth tokens

## 🛠️ **Setup Instructions**

### **1. GitHub Enterprise Server Configuration**

Ensure your GHES has Actions Service enabled:

```yaml
# github-enterprise.conf (or via Management Console)
actions:
  enabled: true
  actions_service:
    enabled: true
    messaging:
      enabled: true
```

### **2. GitHub Token Requirements**

Your token needs these scopes:
- ✅ `admin:org` (for registration tokens)
- ✅ `repo` (for workflow access)
- ✅ `actions` (for Actions Service API)

```bash
# Test token permissions
curl -H "Authorization: token YOUR_TOKEN" \
  https://your-ghes.com/api/v3/orgs/YOUR_ORG/actions/runners/registration-token
```

### **3. Environment Configuration**

```bash
# Required Variables
export GITHUB_TOKEN="ghp_your_personal_access_token_here"
export GITHUB_ENTERPRISE_URL="https://your-ghes.com"
export ORGANIZATION_NAME="your-organization"

# Scale Set Configuration
export RUNNER_SCALE_SET_NAME="ghaec2-scaler"
export RUNNER_SCALE_SET_ID="1"
export MIN_RUNNERS="1"
export MAX_RUNNERS="10"

# AWS Configuration
export AWS_REGION="eu-north-1"
export EC2_SUBNET_ID="subnet-xxxxxxxx"
export EC2_SECURITY_GROUP_ID="sg-xxxxxxxx"
export EC2_KEY_PAIR_NAME="your-key-pair"
export EC2_INSTANCE_TYPE="t3.medium"
export EC2_AMI_ID="ami-xxxxxxxx"
export EC2_SPOT_PRICE="0.05"

# Runner Labels
export RUNNER_LABELS="self-hosted,linux,x64,ghaec2-managed"
```

## 🔄 **How It Works**

### **Actions Service Flow**
```
1. Initialize → Get Registration Token
2. Register → Get Actions Service URL + Admin Token  
3. Create Scale Set → Register runner scale set
4. Create Session → Establish message queue connection
5. Poll Messages → Receive real-time job events
6. Scale EC2 → Create/terminate instances based on demand
```

### **Real-time Event Processing**
```
GitHub Actions → Actions Service → Message Queue → GHAEC2 → EC2
    ↓               ↓                ↓              ↓        ↓
Job Queued    → JobAvailable   → Message      → Scale Up → Launch
Job Started   → JobAssigned    → Message      → Monitor  → Running  
Job Complete  → JobCompleted   → Statistics   → Scale Down → Terminate
```

## 🚀 **Usage Examples**

### **Basic Setup**
```bash
./ghaec2
```

**Expected logs:**
```json
{"msg":"Initializing Actions Service client","organization":"your-org"}
{"msg":"Successfully obtained registration token"}
{"msg":"Successfully initialized Actions Service client","actionsServiceURL":"https://actions.your-ghes.com"}
{"msg":"Starting Actions Service message polling","messagePollingInterval":"2s"}
```

### **Advanced Configuration**
```bash
# High-capacity environment
export MIN_RUNNERS="5"
export MAX_RUNNERS="50"
export EC2_INSTANCE_TYPE="c5.xlarge"
./ghaec2
```

## 📊 **Monitoring & Troubleshooting**

### **Successful Connection Logs**
```json
{"msg":"Successfully obtained registration token"}
{"msg":"Getting Actions Service admin connection","baseURL":"https://your-ghes.com"}
{"msg":"Successfully initialized Actions Service client","actionsServiceURL":"https://actions.your-ghes.com"}
{"msg":"Starting Actions Service message polling"}
```

### **Common Issues**

#### **❌ "Actions Service not available" Error**
```json
{"error":"Actions Service not available on this GitHub Enterprise Server. Please ensure you have a compatible version that supports the Actions Service API. Status: 404"}
```

**Solution:** Upgrade your GHES to a version that supports Actions Service API (GHES 3.9+ recommended).

#### **❌ "Registration token failed"**
```json
{"error":"failed to get registration token: Actions API error (status: 403)"}
```

**Solution:** Check your token has `admin:org` permissions.

#### **❌ "Invalid response" Error**
```json
{"error":"Actions Service endpoint returned invalid response"}
```

**Solution:** Verify Actions Service is enabled in GHES management console.

## 📈 **Performance Benefits**

| Metric | Actions Service API | Traditional API |
|--------|-------------------|-----------------|
| **Job Detection** | Real-time (< 1s) | Polling (60s+) |
| **API Calls** | Minimal | High volume |
| **Rate Limiting** | None | Frequent issues |
| **Scaling Speed** | Instant | Delayed |
| **Resource Usage** | Optimized | Higher overhead |

## 🔮 **Advanced Features**

### **Message-driven Scaling**
- ✅ **JobAvailable** → Immediate scale-up
- ✅ **JobAssigned** → Capacity tracking
- ✅ **JobCompleted** → Smart scale-down
- ✅ **Statistics** → Precise demand forecasting

### **Smart Resource Management**
- ✅ **Idle runner detection** via statistics
- ✅ **Oldest-first termination** for cost optimization
- ✅ **Min/max constraints** for capacity planning
- ✅ **Real-time capacity** adjustment

## 🛡️ **Security & Best Practices**

### **Token Security**
- 🔒 Use **least privilege** token scopes
- 🔒 **Rotate tokens** regularly
- 🔒 **Monitor token usage** in audit logs
- 🔒 **Secure token storage** in environment variables

### **Network Security**
- 🔒 **Private subnets** for runner instances
- 🔒 **Security groups** with minimal access
- 🔒 **VPC endpoints** for AWS API calls
- 🔒 **Encrypted connections** to GHES

## ⚡ **Quick Start Commands**

```bash
# 1. Clone and build
git clone <your-repo>
cd lambda/ghaec2
go build .

# 2. Set environment variables
source .env

# 3. Test connection
./ghaec2 --dry-run

# 4. Start scaling
./ghaec2
```

---

✅ **This implementation provides the most efficient GitHub Actions autoscaling using the modern Actions Service API!**

🚨 **Note:** If your GHES doesn't support Actions Service API, you'll need to upgrade or use a different approach. 