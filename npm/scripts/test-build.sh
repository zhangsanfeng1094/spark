#!/bin/bash
# 本地测试 CI 构建流程
# 用法: ./npm/scripts/test-build.sh [version]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NPM_DIR="$(dirname "$SCRIPT_DIR")"
ROOT_DIR="$(dirname "$NPM_DIR")"

# 版本号（默认从 package.json 读取，可传入参数覆盖）
VERSION="${1:-$(node -p "require('$NPM_DIR/package.json').version")}"
echo "测试版本: $VERSION"

# ============================================================
# Step 1: 构建 Go 二进制（可选，用于测试）
# ============================================================
build_binary() {
  echo ""
  echo "=== Step 1: 构建 Go 二进制 ==="
  
  cd "$ROOT_DIR"
  
  # 检查是否已存在二进制
  for platform in "linux-amd64" "linux-arm64" "darwin-amd64" "darwin-arm64" "windows-amd64.exe" "windows-arm64.exe"; do
    binary_path="$NPM_DIR/dist/spark-$platform"
    if [[ "$platform" == windows* ]]; then
      binary_path="$NPM_DIR/dist/spark-$platform"
    else
      binary_path="$NPM_DIR/dist/spark-$platform"
    fi
    
    if [[ -f "$binary_path" ]]; then
      echo "✓ 已存在: $binary_path"
    else
      echo "✗ 缺失: $binary_path"
    fi
  done
  
  read -p "是否构建 Go 二进制？(y/N) " -n 1 -r
  echo
  if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "构建中..."
    
    # 创建 dist 目录
    mkdir -p "$NPM_DIR/dist"
    
    # Linux amd64
    echo "构建 spark-linux-amd64..."
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$NPM_DIR/dist/spark-linux-amd64" ./cmd/spark
    
    # Linux arm64
    echo "构建 spark-linux-arm64..."
    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "$NPM_DIR/dist/spark-linux-arm64" ./cmd/spark
    
    # Darwin amd64
    echo "构建 spark-darwin-amd64..."
    CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o "$NPM_DIR/dist/spark-darwin-amd64" ./cmd/spark
    
    # Darwin arm64
    echo "构建 spark-darwin-arm64..."
    CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o "$NPM_DIR/dist/spark-darwin-arm64" ./cmd/spark
    
    # Windows amd64
    echo "构建 spark-windows-amd64.exe..."
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o "$NPM_DIR/dist/spark-windows-amd64.exe" ./cmd/spark
    
    # Windows arm64
    echo "构建 spark-windows-arm64.exe..."
    CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -o "$NPM_DIR/dist/spark-windows-arm64.exe" ./cmd/spark
    
    echo "✓ Go 二进制构建完成"
  fi
}

# ============================================================
# Step 2: 运行 build-packages.js
# ============================================================
build_packages() {
  echo ""
  echo "=== Step 2: 运行 build-packages.js ==="
  
  cd "$NPM_DIR"
  
  # 设置环境变量使用本地二进制
  export SPARK_BINARY_BASE_URL="file://$(pwd)/dist"
  
  echo "运行: node scripts/build-packages.js"
  node scripts/build-packages.js
  
  echo "✓ build-packages.js 完成"
}

# ============================================================
# Step 3: 验证生成的文件
# ============================================================
verify_packages() {
  echo ""
  echo "=== Step 3: 验证生成的文件 ==="
  
  cd "$NPM_DIR"
  
  echo ""
  echo "--- packages/ 目录结构 ---"
  find packages -type f 2>/dev/null | sort
  
  echo ""
  echo "--- 主包 package.json ---"
  cat package.json | head -20
  
  echo ""
  echo "--- 平台包示例 (linux-x64) ---"
  cat packages/linux-x64/package.json 2>/dev/null || echo "文件不存在"
  
  echo ""
  echo "--- 检查二进制文件 ---"
  for platform in "linux-x64/spark" "linux-arm64/spark" "darwin-x64/spark" "darwin-arm64/spark" "windows-x64/spark.exe" "windows-arm64/spark.exe"; do
    binary_path="packages/$platform"
    if [[ -f "$binary_path" ]]; then
      size=$(stat -c%s "$binary_path" 2>/dev/null || stat -f%z "$binary_path" 2>/dev/null)
      echo "✓ $binary_path ($(numfmt --to=iec $size 2>/dev/null || echo $size) bytes)"
    else
      echo "✗ 缺失: $binary_path"
    fi
  done
}

# ============================================================
# Step 4: 模拟 npm pack（验证包内容）
# ============================================================
pack_packages() {
  echo ""
  echo "=== Step 4: 模拟 npm pack ==="
  
  cd "$NPM_DIR"
  
  read -p "是否运行 npm pack --dry-run？(y/N) " -n 1 -r
  echo
  if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo ""
    echo "--- 主包 ---"
    npm pack --dry-run 2>&1 | head -20
    
    echo ""
    for dir in packages/*/; do
      if [[ -d "$dir" ]]; then
        echo "--- $(basename $dir) ---"
        cd "$dir"
        npm pack --dry-run 2>&1 | head -10
        cd - > /dev/null
      fi
    done
  fi
}

# ============================================================
# 主流程
# ============================================================
main() {
  echo "=============================================="
  echo "本地测试 CI 构建流程"
  echo "版本: $VERSION"
  echo "=============================================="
  
  build_binary
  build_packages
  verify_packages
  pack_packages
  
  echo ""
  echo "=============================================="
  echo "✓ 测试完成"
  echo "=============================================="
}

main
