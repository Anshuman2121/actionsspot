# Bootstrap Lambda Issue - Complete Solution Guide

## 🚨 Issue: Persistent Runtime.InvalidEntrypoint Error

**Error:** `Couldn't find valid bootstrap(s): [/var/task/bootstrap /opt/bootstrap]`

After multiple attempts with standard fixes, this appears to be a more complex issue. Here are **5 different approaches** to try:

## 🔧 Solution 1: Standard Bootstrap Approach (RETRY)

```bash
cd lambda/github-runner-scaler

# Clean rebuild
rm -f bootstrap github-runner-scaler.zip

# Build with explicit flags
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -a -installsuffix cgo -ldflags '-w -s' -o bootstrap .
chmod +x bootstrap

# Create zip
zip github-runner-scaler.zip bootstrap
cp github-runner-scaler.zip terraform/
```

**Terraform config:**
```hcl
resource "aws_lambda_function" "github_runner_scaler" {
  filename      = "github-runner-scaler.zip"
  function_name = "github-runner-scaler"
  handler       = "bootstrap"
  runtime       = "provided.al2"
  architectures = ["x86_64"]
  memory_size   = 512
}
```

## 🔧 Solution 2: Use Different Runtime

```hcl
resource "aws_lambda_function" "github_runner_scaler" {
  # ... same config but different runtime
  runtime = "provided.al2023"  # Try newer runtime
}
```

## 🔧 Solution 3: Use Container Image Instead

Create `Dockerfile`:
```dockerfile
FROM public.ecr.aws/lambda/provided:al2

COPY bootstrap ${LAMBDA_RUNTIME_DIR}
CHMOD +x ${LAMBDA_RUNTIME_DIR}/bootstrap

CMD ["bootstrap"]
```

Build and deploy:
```bash
# Build container
docker build -t github-runner-scaler .

# Tag for ECR
docker tag github-runner-scaler:latest 123456789012.dkr.ecr.region.amazonaws.com/github-runner-scaler:latest

# Push to ECR (requires aws ecr get-login-password)
docker push 123456789012.dkr.ecr.region.amazonaws.com/github-runner-scaler:latest
```

Terraform:
```hcl
resource "aws_lambda_function" "github_runner_scaler" {
  package_type = "Image"
  image_uri    = "123456789012.dkr.ecr.region.amazonaws.com/github-runner-scaler:latest"
  function_name = "github-runner-scaler"
  role         = aws_iam_role.lambda_role.arn
  timeout      = 900
  memory_size  = 512
}
```

## 🔧 Solution 4: Use go1.x Runtime with Handler

Build as a regular Go binary:
```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o main .
zip github-runner-scaler.zip main
```

Terraform:
```hcl
resource "aws_lambda_function" "github_runner_scaler" {
  filename      = "github-runner-scaler.zip"
  function_name = "github-runner-scaler"
  handler       = "main"
  runtime       = "go1.x"  # Deprecated but might work
  memory_size   = 512
}
```

## 🔧 Solution 5: Create Simple Wrapper Shell Script

Create wrapper approach:
```bash
# Create wrapper script
cat > wrapper.sh << 'EOF'
#!/bin/bash
exec ./bootstrap "$@"
EOF

chmod +x wrapper.sh
zip github-runner-scaler.zip bootstrap wrapper.sh
```

Terraform:
```hcl
resource "aws_lambda_function" "github_runner_scaler" {
  filename      = "github-runner-scaler.zip"
  function_name = "github-runner-scaler"
  handler       = "wrapper.sh"
  runtime       = "provided.al2"
}
```

## 🔧 Solution 6: Use Lambda Layer

Put bootstrap in a layer:
```bash
mkdir -p layer/bin
cp bootstrap layer/bin/
cd layer
zip -r ../bootstrap-layer.zip .
cd ..
```

Terraform:
```hcl
resource "aws_lambda_layer_version" "bootstrap_layer" {
  filename   = "bootstrap-layer.zip"
  layer_name = "github-runner-bootstrap"
  compatible_runtimes = ["provided.al2"]
  compatible_architectures = ["x86_64"]
}

resource "aws_lambda_function" "github_runner_scaler" {
  filename      = "empty.zip"  # Empty zip
  function_name = "github-runner-scaler"
  handler       = "bootstrap"
  runtime       = "provided.al2"
  layers        = [aws_lambda_layer_version.bootstrap_layer.arn]
}
```

## 🔧 Solution 7: Debug with Simple Test Function

Create minimal test:
```go
// test_minimal.go
package main

import (
    "context"
    "github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event interface{}) (string, error) {
    return "success", nil
}

func main() {
    lambda.Start(handler)
}
```

Build and test:
```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bootstrap test_minimal.go
zip test.zip bootstrap
# Deploy this simple version first to verify Lambda works
```

## 🎯 Recommended Approach

1. **Try Solution 1** (standard bootstrap) with `provided.al2023` runtime
2. If that fails, try **Solution 3** (container image)
3. As last resort, try **Solution 4** (go1.x runtime)

## 📞 If All Solutions Fail

This might indicate:
- AWS account/region specific limitations
- IAM permission issues
- AWS Lambda service problems
- VPC/networking configuration issues

Check:
1. AWS CloudTrail logs for detailed errors
2. VPC configuration if Lambda is in VPC
3. IAM permissions for Lambda execution
4. Regional Lambda service status

## 🔄 Next Steps

Choose one solution and implement it. The container approach (Solution 3) is most reliable for complex Go applications. 