# Antigravity for fnOS

将 **Google Antigravity CLI (`agy`)** 深度集成到 **飞牛私有云（fnOS）** 的原生 `.fpk` 应用，采用 **Web Chat 图形对话 + 内置 Web 终端（TUI）** 双引擎混合架构。

## 核心特性

- **Go 单二进制零依赖**：前端 SPA 通过 `go:embed` 内嵌，无需宿主机预装 Node.js / Python，支持 `x86`（`amd64`）与 `arm`（`arm64`）。
- **双引擎混合交互**：
  - **Web Chat**：封装 `agy -p --output-format stream-json`，通过 SSE 实时渲染 Markdown、思考过程（Thinking）折叠块、工具调用卡片、Token 统计与多会话历史。
  - **内置 Web 终端**：基于 `xterm.js` + WebSocket + `creack/pty`，支持首次无头 NAS 的 `agy` 交互式 OAuth 登录、使用 `/mcp` `/skills` `/diff` 等原生 TUI 面板，以及从 Web Chat 一键切入终端继续同一会话。
- **NAS 专属适配**：
  - 专用应用用户（`run-as: "package"`，用户 `antigravity-cli`），安全隔离，支持在飞牛应用设置中按需授权共享目录。
  - 内置 NAS 目录选择器（自动识别 `TRIM_DATA_SHARE_PATHS` 与 `/vol*` 存储卷），支持为对话绑定主工作区与 `--add-dir` 附加目录。
  - 内置应用级代理配置（`HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY`）与连通性测试，不影响宿主机全局网络。
  - `agy` 二进制自动探测与一键安装/升级（实时流式日志），所有数据与登录态持久化在 `${TRIM_PKGVAR}`，升级不丢状态。
- **安全保护**：首次访问强制设置应用密码（`bcrypt` 哈希 + 登录防爆破限流 + `HttpOnly` Cookie + 一次性 WebSocket Ticket）。

## 目录结构

```text
├── backend/                # Go 1.24 后端（鉴权、代理、agy 管理、Chat SSE、PTY WS、目录浏览）
├── frontend/               # React 19 + TypeScript + Vite + Tailwind v4 + xterm.js
├── fpk/                    # fnOS 原生应用包模板（manifest、config/privilege、cmd/main 等）
├── scripts/build.sh        # 一键构建前端、交叉编译 Go 与组装 FPK
└── .github/workflows/      # GitHub Actions 自动构建 CI
```

## 快速开发与运行

### 1. 构建并启动单二进制服务

```bash
# 构建前端并输出到 backend/internal/server/dist
cd frontend && npm install && npm run build

# 编译并启动后端（默认监听 :18765，数据保存在 ./.local-data）
cd ../backend && go run .
```

打开浏览器访问 `http://localhost:18765`，首次进入设置访问密码即可体验。

### 2. 打包 fnOS 原生应用（`.fpk`）

```bash
# 仅构建 x86 (amd64)
bash scripts/build.sh amd64

# 同时构建 x86 (amd64) 与 arm (arm64)
bash scripts/build.sh all
```

如果环境中已安装飞牛官方 `fnpack` CLI，脚本会自动在 `dist/` 下产出 `antigravity-cli-x86.fpk` / `antigravity-cli-arm.fpk`；若未安装 `fnpack`，则会在 `dist/stage-x86`、`dist/stage-arm` 生成完整的待打包目录。

## 在飞牛 fnOS 上使用

1. 在飞牛「应用中心 -> 手动安装」上传生成的 `.fpk` 包。
2. 安装后可在「应用中心 -> Antigravity -> 应用设置」中，为专用用户 `antigravity-cli` 按需勾选需要读写的 NAS 共享文件夹。
3. 打开桌面图标（默认端口 `18765`），设置访问密码。
4. 前往「设置中心」配置网络代理（如需）并点击**一键安装 `agy`**。
5. 切换到「内置 Web 终端」完成首次 `agy` 交互式登录，随后即可在「Web Chat」与「Web 终端」之间无缝切换使用。