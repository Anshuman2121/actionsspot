# 📥 **Install Go on macOS**

You need Go installed to build the Lambda function. Here are the easiest ways:

## 🍺 **Option 1: Using Homebrew (Recommended)**

```bash
# Install Homebrew if you don't have it
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

# Install Go
brew install go

# Verify installation
go version
```

## 📦 **Option 2: Direct Download**

1. **Download Go**: Visit https://golang.org/dl/
2. **Download**: `go1.21.x.darwin-amd64.pkg` (for Intel Mac) or `go1.21.x.darwin-arm64.pkg` (for M1/M2 Mac)
3. **Install**: Double-click the downloaded file and follow instructions
4. **Add to PATH**: Add this to your `~/.zshrc` or `~/.bashrc`:
   ```bash
   export PATH=$PATH:/usr/local/go/bin
   ```
5. **Reload shell**: `source ~/.zshrc`
6. **Verify**: `go version`

## 🔧 **Option 3: Using g (Go Version Manager)**

```bash
# Install g
curl -sSL https://git.io/g-install | sh -s

# Install latest Go
g install latest

# Verify
go version
```

## ✅ **Verification**

After installation, verify Go is working:

```bash
go version
# Should output: go version go1.21.x darwin/amd64 (or darwin/arm64)

go env GOPATH
# Should output your Go workspace path
```

## 🚀 **Next Steps**

Once Go is installed, you can build the Lambda function:

```bash
cd lambda/github-runner-scaler
./deploy.sh build-only
```

## 🔧 **Troubleshooting**

### **"go: command not found"**
- Make sure Go is in your PATH: `echo $PATH`
- Add Go to PATH: `export PATH=$PATH:/usr/local/go/bin`
- Restart your terminal

### **Permission Issues**
```bash
# Fix permissions if needed
sudo chown -R $(whoami) /usr/local/go
```

### **Multiple Go Versions**
```bash
# Check which Go you're using
which go

# Remove old versions if needed
sudo rm -rf /usr/local/go

# Then reinstall with your preferred method
``` 