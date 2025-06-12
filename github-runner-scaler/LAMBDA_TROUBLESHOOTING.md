# Lambda Bootstrap Troubleshooting Guide

## 🚨 Current Issue: Runtime.InvalidEntrypoint

**Error:** `Couldn't find valid bootstrap(s): [/var/task/bootstrap /opt/bootstrap]`

## 🔍 Troubleshooting Steps

### Step 1: Verify Package Contents
```bash
cd lambda/github-runner-scaler
unzip -Z github-runner-scaler.zip

# Should show:
# -rwxr-xr-x  3.0 unx 11227320 bx defN 25-Jun-12 16:57 bootstrap
```

### Step 2: Test Binary Extraction
```bash
mkdir test && cd test
unzip ../github-runner-scaler.zip
ls -la bootstrap
file bootstrap

# Expected output:
# -rwxr-xr-x  1 user  staff  11227320 bootstrap
# bootstrap: ELF 64-bit LSB executable, x86-64, ...
```

### Step 3: Verify Terraform Configuration
Check these settings in `terraform/main.tf`:
```hcl
resource "aws_lambda_function" "github_runner_scaler" {
  filename         = "github-runner-scaler.zip"  # ✅ Correct path
  handler         = "bootstrap"                   # ✅ Matches binary name
  runtime         = "provided.al2"               # ✅ Correct for Go
  memory_size      = 512                         # ✅ Sufficient memory
  architectures    = ["x86_64"]                  # ✅ Matches binary arch
  timeout         = 900
}
```

### Step 4: Deploy with Correct Settings
```bash
cd terraform
terraform plan
terraform apply
```

### Step 5: Manual AWS CLI Test (Alternative)
If Terraform continues to fail, try manual deployment:
```bash
# Create function
aws lambda create-function \
  --function-name github-runner-scaler-test \
  --runtime provided.al2 \
  --role arn:aws:iam::YOUR_ACCOUNT:role/lambda-execution-role \
  --handler bootstrap \
  --zip-file fileb://github-runner-scaler.zip \
  --architectures x86_64 \
  --memory-size 512 \
  --timeout 900

# Or update existing
aws lambda update-function-code \
  --function-name github-runner-scaler \
  --zip-file fileb://github-runner-scaler.zip \
  --architectures x86_64
```

### Step 6: Check Lambda Logs
```bash
aws logs tail /aws/lambda/github-runner-scaler --follow
```

## 🛠️ Common Fixes

### Fix 1: Rebuild with Explicit Permissions
```bash
cd lambda/github-runner-scaler
rm -f github-runner-scaler.zip bootstrap
./deploy.sh build-only
cp github-runner-scaler.zip terraform/
```

### Fix 2: Use Different Zip Method
```bash
# Alternative zip method
rm -f github-runner-scaler.zip
chmod +x bootstrap
zip -X github-runner-scaler.zip bootstrap
```

### Fix 3: Check File Integrity
```bash
# Verify binary is not corrupted
ldd bootstrap 2>/dev/null || echo "Statically linked (good for Lambda)"
strings bootstrap | grep -i "go build" | head -5
```

### Fix 4: Use Lambda Layer (If All Else Fails)
Create a Lambda layer instead:
```bash
mkdir -p layer/bin
cp bootstrap layer/bin/
cd layer
zip -r ../bootstrap-layer.zip .
```

Then in Terraform:
```hcl
resource "aws_lambda_layer_version" "bootstrap_layer" {
  filename   = "bootstrap-layer.zip"
  layer_name = "github-runner-bootstrap"
  compatible_runtimes = ["provided.al2"]
  compatible_architectures = ["x86_64"]
}

resource "aws_lambda_function" "github_runner_scaler" {
  # ... other config ...
  layers = [aws_lambda_layer_version.bootstrap_layer.arn]
}
```

## 📋 Environment Variables Check

Ensure these are set in Lambda:
```
GITHUB_TOKEN=ghp_xxxxx
GITHUB_ENTERPRISE_URL=https://TelenorSwedenAB.ghe.com
ORGANIZATION_NAME=TelenorSweden
EC2_AMI_ID=ami-xxxxx
EC2_SUBNET_ID=subnet-xxxxx
EC2_SECURITY_GROUP_ID=sg-xxxxx
EC2_KEY_PAIR_NAME=your-key
```

## 🎯 Expected Success

After fixing, you should see:
```
START RequestId: xxx Version: $LATEST
🚀 GitHub Runner Scaler Lambda triggered at 2025-06-12T16:58:00Z
✅ Lambda execution completed successfully
END RequestId: xxx
```

## 📞 Next Steps

1. **Try rebuilding** with the updated deploy script
2. **Redeploy** with the corrected Terraform configuration
3. **Check CloudWatch logs** for detailed error messages
4. **Test manually** with AWS CLI if needed

If the issue persists, it may be an AWS account/region specific problem or Lambda service limitation. 