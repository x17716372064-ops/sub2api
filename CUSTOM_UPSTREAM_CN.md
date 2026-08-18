# 自定义版本的上游同步与更新

本仓库采用“官方 `main` + 独立自定义提交”的维护方式。不要在 VPS 上直接安装
`Wei-Shaw/sub2api` 的官方二进制，否则自定义功能会被覆盖。

## 推荐分支结构

- `origin/main`：自己的自定义稳定分支。
- `upstream/main`：`Wei-Shaw/sub2api` 官方主分支。
- 自定义功能保持为小而独立的提交，数据库变更只新增迁移文件。

## 自动同步

Fork 到自己的 GitHub 仓库后，启用 Actions，并允许 Actions 对仓库写入。
`Custom Upstream Sync` 每天拉取并合并官方 `main`，执行前端类型检查、构建和后端测试。
验证通过后会推送合并结果并创建形如 `v0.1.177+custom.12` 的标签。

`Custom Release` 会为该标签构建 Linux AMD64 二进制、校验文件和 GitHub Release。
发生合并冲突或测试失败时不会发布，需要在本地处理后再重新运行工作流。

## 更新器配置

自定义版本初次部署应使用：

```env
UPDATE_REPOSITORY=Wei-Shaw/sub2api
UPDATE_ALLOW_IN_PLACE=false
```

这样仍能看到官方的新版本提示，但“立即更新”不会覆盖自定义代码。

自己的 Fork 已成功生成 Custom Release 后，改为：

```env
UPDATE_REPOSITORY=<你的 GitHub 用户名>/sub2api
UPDATE_ALLOW_IN_PLACE=true
```

此后内置更新器只会下载自己仓库中包含扩展功能的 Release。

## 本地同步

自动同步失败时，在干净工作区执行：

```bash
git remote add upstream https://github.com/Wei-Shaw/sub2api.git
git fetch upstream main --tags
git merge upstream/main
```

解决冲突后重新运行测试。不要强制覆盖数据库卷；部署前保留 PostgreSQL、Redis 和当前
可运行镜像的备份。
