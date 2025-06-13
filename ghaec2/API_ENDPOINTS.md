# GitHub Actions EC2 Scaler - API Endpoints Used for Polling

This document lists all the APIs that the `ghaec2` scaler uses to poll for workflow requests and manage scaling.

## 🔗 **Core Polling APIs**

### 1. **Message Queue Polling** (Primary Polling Method)
```
GET {actionsServiceURL}/_apis/runtime/runnerscalesets/{scaleSetId}/messages
```
- **Purpose**: Long-polling for real-time workflow job messages
- **Method**: `GET`
- **Headers**:
  - `Accept: application/json; api-version=6.0-preview`
  - `Authorization: Bearer {messageQueueAccessToken}`
  - `X-GitHub-Actions-Scale-Set-Max-Capacity: {maxCapacity}`
- **Query Parameters**:
  - `sessionId={sessionId}` (automatically included in URL)
  - `lastMessageId={lastMessageId}` (when > 0)
  - `api-version=6.0-preview`
- **Timeout**: 5 minutes (long-polling)
- **Returns**: `RunnerScaleSetMessage` with job statistics and events

### 2. **Acquirable Jobs** (Fallback/Additional Info)
```
GET {actionsServiceURL}/_apis/runtime/runnerscalesets/{scaleSetId}/acquirablejobs
```
- **Purpose**: Get list of jobs that can be acquired by the scale set
- **Method**: `GET`
- **Headers**:
  - `Authorization: Bearer {adminToken}`
  - `Content-Type: application/json`
- **Query Parameters**:
  - `api-version=6.0-preview`
- **Returns**: `AcquirableJobList` with available jobs

## 🔧 **Setup & Management APIs**

### 3. **GitHub Token Verification**
```
GET {githubEnterpriseURL}/api/v3/user
```
- **Purpose**: Verify GitHub token validity and permissions
- **Method**: `GET`
- **Headers**:
  - `Authorization: Bearer {githubToken}`
  - `Content-Type: application/vnd.github.v3+json`

### 4. **Organization Access Check**
```
GET {githubEnterpriseURL}/api/v3/orgs/{organization}
```
- **Purpose**: Verify token has access to organization
- **Method**: `GET`
- **Headers**:
  - `Authorization: Bearer {githubToken}`
  - `Content-Type: application/vnd.github.v3+json`

### 5. **Actions Permissions Check**
```
GET {githubEnterpriseURL}/api/v3/orgs/{organization}/actions/permissions
```
- **Purpose**: Verify token has Actions API permissions
- **Method**: `GET`
- **Headers**:
  - `Authorization: Bearer {githubToken}`
  - `Content-Type: application/vnd.github.v3+json`

### 6. **Registration Token**
```
POST {githubEnterpriseURL}/api/v3/orgs/{organization}/actions/runners/registration-token
```
- **Purpose**: Get registration token for Actions Service access
- **Method**: `POST`
- **Headers**:
  - `Authorization: Bearer {githubToken}`
  - `Content-Type: application/vnd.github.v3+json`
- **Returns**: Registration token for Actions Service

### 7. **Actions Service Admin Connection**
```
POST {githubEnterpriseURL}/api/v3/actions/runner-registration
```
- **Purpose**: Get Actions Service URL and admin token
- **Method**: `POST`
- **Headers**:
  - `Authorization: Bearer {registrationToken}`
  - `Content-Type: application/json`
- **Body**: `{"url": "{registrationURL}"}`
- **Returns**: Actions Service URL and admin token

### 8. **Runner Scale Set Creation/Retrieval**
```
GET {actionsServiceURL}/_apis/runtime/runnerscalesets
POST {actionsServiceURL}/_apis/runtime/runnerscalesets
```
- **Purpose**: Get or create runner scale set
- **Method**: `GET` (check existing) / `POST` (create new)
- **Headers**:
  - `Authorization: Bearer {adminToken}`
  - `Content-Type: application/json`
- **Query Parameters**:
  - `api-version=6.0-preview`

### 9. **Message Session Management**
```
POST {actionsServiceURL}/_apis/runtime/runnerscalesets/{scaleSetId}/sessions
```
- **Purpose**: Create message session for real-time polling
- **Method**: `POST`
- **Headers**:
  - `Authorization: Bearer {adminToken}`
  - `Content-Type: application/json`
- **Body**: `{"ownerName": "{hostname}", "runnerScaleSet": {...}}`
- **Returns**: Session with message queue URL and access token

## 🎯 **Job Management APIs**

### 10. **Acquire Jobs**
```
POST {actionsServiceURL}/_apis/runtime/runnerscalesets/{scaleSetId}/acquirejobs
```
- **Purpose**: Acquire specific jobs for processing
- **Method**: `POST`
- **Headers**:
  - `Authorization: Bearer {messageQueueAccessToken}`
  - `Content-Type: application/json`
- **Body**: Array of job request IDs
- **Returns**: Array of acquired job IDs

### 11. **Delete Message**
```
DELETE {messageQueueURL}
```
- **Purpose**: Delete processed message from queue
- **Method**: `DELETE`
- **Headers**:
  - `Authorization: Bearer {messageQueueAccessToken}`
- **Query Parameters**:
  - `messageId={messageId}`
  - `api-version=6.0-preview`

### 12. **Session Refresh**
```
PATCH {actionsServiceURL}/_apis/runtime/runnerscalesets/{scaleSetId}/sessions/{sessionId}
```
- **Purpose**: Refresh expired message session
- **Method**: `PATCH`
- **Headers**:
  - `Authorization: Bearer {adminToken}`
- **Query Parameters**:
  - `api-version=6.0-preview`

### 13. **Session Cleanup**
```
DELETE {actionsServiceURL}/_apis/runtime/runnerscalesets/{scaleSetId}/sessions/{sessionId}
```
- **Purpose**: Delete message session on shutdown
- **Method**: `DELETE`
- **Headers**:
  - `Authorization: Bearer {adminToken}`
- **Query Parameters**:
  - `api-version=6.0-preview`

## 📊 **Polling Flow**

1. **Initialization**: APIs 3-9 (setup and authentication)
2. **Main Polling Loop**: API 1 (message queue polling) - **PRIMARY**
3. **Job Processing**: APIs 10-11 (acquire and delete messages)
4. **Session Management**: APIs 12-13 (refresh/cleanup)
5. **Fallback**: API 2 (acquirable jobs) - if needed

## 🔄 **Polling Frequency**

- **Message Queue Polling**: Continuous long-polling (5-minute timeout)
- **Session Refresh**: Only when tokens expire
- **Acquirable Jobs**: On-demand or as fallback

## 🌐 **Example URLs**

Based on your configuration:
- **GitHub Enterprise**: `https://TelenorSwedenAB.ghe.com`
- **Actions Service**: `https://pipelinesproxsdc1.actions.githubusercontent.com/...`
- **Organization**: `TelenorSweden`
- **Scale Set ID**: `1`

The primary polling endpoint would be:
```
GET https://pipelinesproxsdc1.actions.githubusercontent.com/1wV8PME5MpPuE6dWDHM0qQAg2HEKLGQQC1KgkaGqPT3lXRGrc7/_apis/runtime/runnerscalesets/1/messages?sessionId=cc05a785-fbd7-4ceb-af8f-7709eab4dd7b&api-version=6.0-preview
``` 