> 看 https://github.com/wes-lin/Cloud189Checkin 在 github action上好像挂了,一直提示让手动运行
>
> 搞一个部署到 vercel 上,token 复用暂时使用 gist

# cloud189-checkin

天翼云盘个人签到 + 家庭签到，Go 单二进制，部署在 Vercel（`cmd/api/main.go` 常驻模式）。

有前端查看 log/curd 账号

## 功能

- 个人签到 `userSign`，家庭签到 `getFamilyList + familyUserSign`（失败只告警不中断）
- 账号密码首次登录后复用 token：`189_tokens.json` 存 Gist，优先 `accessToken`，其次 `refreshToken` 刷新，最后才密码登录
- 前端页面管理账号、手动触发签到、查看日志

## 环境变量（3 个）

| 变量 | 说明 |
|---|---|
| `ACCOUNTS_189` | 冷启动种子，`138xxx:pass1,139xxx:pass2`，逗号或换行分隔，首次自动写入 Gist |
| `GH_TOKEN`（或 `GITHUB_TOKEN`，前者优先） | 仅 `gist` 权限的 PAT |
| `GIST_ID` | 私密 Gist ID |

Gist 内固定 3 个文件：`189_accounts.json`、`189_tokens.json`、`189_logs.json`（保留最近 200 条）。

未填 `GH_TOKEN`/`GITHUB_TOKEN` / `GIST_ID` 任一时：token 与日志功能关闭，每次都用账号密码登录，
账号只读 `ACCOUNTS_189`，前端的账号管理与日志不可用。

前端登录：用任一账号的用户名+密码即可（验 Gist 中的账号），session 为 HMAC Cookie（key=`GH_TOKEN`/`GITHUB_TOKEN`），无状态。

## 本地运行

```sh
export ACCOUNTS_189='138xxx:pass'
export GH_TOKEN='xxx'
export GIST_ID='xxx'
make run
# 打开 http://localhost:3000
```

## 部署

Vercel 一键导入本仓库，填好 3 个环境变量即可，定时任务见 `vercel.json`（每天 01:00 调 `/api/checkin`）。

## 接口

- `POST /api/login` `{username,password}` 写 session cookie
- `GET/POST/PUT/DELETE /api/accounts` 需登录，GET 脱敏返回
- `GET /api/logs?skip=&limit=` 需登录
- `GET /api/checkin` cron 用；`POST /api/checkin` 手动触发（需登录）
