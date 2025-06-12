# 🧹 Enhanced Instance Cleanup After Pipeline Completion

## Overview
The GHAEC2 scaler now includes comprehensive cleanup functionality to automatically terminate EC2 instances when GitHub Actions pipelines are completed and no longer needed.

## 🎯 Key Features

### 1. **Continuous Job Monitoring**
- **Dual Polling System**: Messages every 2 seconds + job checks every 60 seconds
- **Proactive Detection**: Checks for pending jobs even without message activity
- **Real-time Response**: Immediate scaling when jobs are detected
- **Fallback Mode Optimization**: Enhanced monitoring for older GitHub Enterprise Server

### 2. **Automatic Scale-Down**
- **Smart Detection**: Monitors for completed pipelines and idle runners
- **Fallback Mode Optimization**: More aggressive cleanup in fallback mode when no jobs are pending
- **Age-Based Termination**: Terminates oldest instances first to optimize costs

### 3. **Configurable Cleanup Behavior**
```bash
# Environment Variables for Cleanup Control
IDLE_TIMEOUT_MINUTES=30          # How long before considering instances idle (default: 30)
CLEANUP_INTERVAL_SECONDS=60      # How often to check for jobs and cleanup (default: 60)
AGGRESSIVE_CLEANUP=true          # Enable aggressive cleanup in fallback mode (default: false)
```

### 4. **Multiple Cleanup Strategies**

#### **Statistics-Based Cleanup**
- Uses GitHub Actions statistics to determine runner utilization
- Respects idle runner counts in normal mode
- Maintains minimum runner configuration

#### **Fallback Mode Cleanup**
- Direct job detection via GitHub API
- Immediate cleanup when no jobs are queued
- Aggressive mode for cost optimization

#### **Stale Instance Detection**
- Identifies long-running instances without active jobs
- Prevents resource waste from stuck processes
- Conservative approach to avoid interrupting valid work

## 🔧 How It Works

### Enhanced Polling System
```
┌─ Message Polling (Every 2s) ─┐    ┌─ Job Checking (Every 60s) ─┐
│                              │    │                             │
│ • Check for Actions messages │    │ • Query GitHub API for jobs│
│ • Process job statistics     │    │ • Proactive scaling         │
│ • Handle job events          │    │ • Cleanup decisions         │
│                              │    │                             │
└──────────────────────────────┘    └─────────────────────────────┘
                    ↓                              ↓
                    └──── Combined Scaling Logic ────┘
```

### Scale-Down Decision Logic
```
1. Check pending jobs (available + assigned + fallback detected)
2. Calculate desired runners = max(pending_jobs, min_runners)
3. If current_runners > desired_runners:
   - Normal mode: Only terminate idle runners
   - Fallback mode: Terminate excess runners when no jobs pending
   - Aggressive mode: Immediate cleanup of all excess capacity
```

### Termination Priority
1. **Oldest instances first** - Optimizes for cost savings
2. **Idle runners only** - Preserves active work
3. **Respects minimum configuration** - Maintains baseline capacity

## 📊 Polling & Cleanup Scenarios

### ✅ **Continuous Job Monitoring**
- **Trigger**: Every 60 seconds in fallback mode
- **Action**: Check GitHub API for queued workflows
- **Result**: Proactive scaling regardless of message activity

### ✅ **Pipeline Completion Cleanup**
- **Trigger**: Job statistics show completed work
- **Action**: Scale down to match pending workload
- **Result**: Immediate cost savings

### ✅ **No Pending Jobs (Fallback Mode)**
- **Trigger**: GitHub API shows no queued workflows
- **Action**: Cleanup all excess instances above minimum
- **Result**: Maintain baseline capacity only

### ✅ **Idle Timeout Cleanup**
- **Trigger**: Instances running longer than `IDLE_TIMEOUT_MINUTES`
- **Action**: Conservative monitoring (future enhancement for automatic cleanup)
- **Result**: Prevent resource waste

## 🚀 **Usage Examples**

### Basic Continuous Monitoring
```bash
export MIN_RUNNERS=1
export MAX_RUNNERS=5
export CLEANUP_INTERVAL_SECONDS=60  # Check every minute
export IDLE_TIMEOUT_MINUTES=30
./ghaec2
```

### High-Frequency Monitoring
```bash
export MIN_RUNNERS=0
export MAX_RUNNERS=10
export CLEANUP_INTERVAL_SECONDS=30  # Check every 30 seconds
export AGGRESSIVE_CLEANUP=true      # Immediate cleanup
export IDLE_TIMEOUT_MINUTES=15
./ghaec2
```

### Production Balance
```bash
export MIN_RUNNERS=2                # Always maintain 2 runners
export MAX_RUNNERS=20
export CLEANUP_INTERVAL_SECONDS=60  # Standard monitoring
export IDLE_TIMEOUT_MINUTES=45      # Allow longer jobs
./ghaec2
```

## 📈 **Benefits**

### 💰 **Cost Optimization**
- **Continuous monitoring** prevents missed scaling opportunities
- **Immediate cleanup** when pipelines complete
- **Prevent idle instance costs** from stuck processes  
- **Smart termination order** (oldest first) for maximum savings

### ⚡ **Efficient Resource Management**
- **Proactive scaling** based on actual workload
- **Dual polling system** for maximum responsiveness
- **Fallback mode optimization** for older GitHub Enterprise Server
- **Configurable behavior** for different environments

### 🛡️ **Safe Operations**
- **Conservative defaults** to prevent work interruption
- **Respect minimum configuration** to maintain availability
- **Oldest-first termination** to preserve recent work

## 🔍 **Monitoring & Logging**

The scaler provides detailed logging for polling and cleanup decisions:

```json
{"msg":"Starting polling loops","messagePollingInterval":"2s","jobCheckInterval":"60s"}
{"msg":"Fallback mode: proactive job check"}
{"msg":"Found acquirable jobs via fallback","jobCount":2}
{"msg":"Fallback mode: no pending jobs, cleaning up excess runners","runnersToTerminate":3,"currentRunners":5,"minRunners":2,"aggressiveCleanup":true}
{"msg":"Terminated instances","instanceIds":["i-1234","i-5678","i-9abc"],"count":3}
```

## 🎛️ **Configuration Reference**

| Variable | Default | Description |
|----------|---------|-------------|
| `MIN_RUNNERS` | 0 | Minimum instances to maintain |
| `MAX_RUNNERS` | 10 | Maximum instances allowed |
| `CLEANUP_INTERVAL_SECONDS` | 60 | Job checking and cleanup frequency |
| `IDLE_TIMEOUT_MINUTES` | 30 | Idle threshold for cleanup consideration |
| `AGGRESSIVE_CLEANUP` | false | Enable immediate excess cleanup |

## 🔮 **Future Enhancements**

- **GitHub API Integration**: Query actual runner status for smarter cleanup
- **Dynamic Polling**: Adjust polling frequency based on activity
- **Spot Instance Handling**: Graceful handling of spot interruptions  
- **Custom Cleanup Policies**: Job-type specific cleanup rules
- **Metrics Integration**: CloudWatch metrics for cleanup monitoring

---

✅ **The enhanced polling system ensures your pipelines are detected immediately and instances are cleaned up efficiently, optimizing both performance and costs!** 