# Twitter Media Downloader

轻松，快速，安全，整洁，批量的下载推特上的用户，列表，用户的关注。。。开箱即用！

## Feature

- 下载指定用户的媒体推文 (video, img, gif)
- 保留推文标题
- 以列表为单位批量下载
- 关注中的用户批量下载
- 保留列表/关注结构
- 同步用户/列表信息：名称，是否受保护，等。。。
- 记录用户曾用名
- 避免重复下载
  - 每次工作后记录用户的最新发布时间，下次工作仅从这个时间点开始拉取用户推文
  - 向列表目录发送指向用户目录的快捷方式，无论多少列表包含同一用户，本地仅保存一份用户存档
- 避免重复获取时间线：任意一段时间内的推文仅仅会从 twitter 上拉取一次，即使这些推文下载失败。如果下载失败将它们存储到本地，以待重试或丢弃
- 速率限制：避免触发 Twitter API 速率限制

## How to use

### 更新/填写配置

第一次运行程序请按要求将配置项依次填入

#### 配置项介绍

1. `storeage path`：存储路径(可以不存在)
2. `auth_token`：用于登录，见下文
3. `ct0`：用于登录，见下文

#### 获取 Cookie

1. 使用 Chrome 浏览器打开 https://twitter.com 后，按`F12` 打开开发者控制台
2. 选中顶部 `应用`，并复制对应项的值

​	![ 2024-06-25 093928.png](https://s2.loli.net/2024/06/25/O6PwWGoqYLZAJXc.png)

#### 更新配置

```
tmd2 --conf
```

**执行上述命令将导致引导配置程序重新运行，这将重新配置整个配置文件，而不是单独的配置项。单独修改配置项**请至 `%appdata%/tmd2/conf.yaml` 手动修改

###  用户下载

下载指定用户的媒体推文

`tmd2 --user <uid> | <screen_name>`

```
//eg.
tmd2 --user hello	// 下载 screen_name 为 "hello" 的用户
tmd2 --user 123456	// 下载 user_id 为 123456 的用户
```

![ 2024-06-22 185026.png](https://s2.loli.net/2024/06/22/u45c1nUwHOKtbjE.png "用户的screen_name")

### 列表下载

下载指定列表中每一个用户的推文

`tmd2 --list <list_id>`

```
eg.
tmd2 --list 123456
```

![image-20240622184027270.png](https://s2.loli.net/2024/06/22/M4xmVUkZ6DpPrds.png "list_id")

### 关注列表下载

下载指定用户正关注的每个用户的推文

`tmd2 --foll <uid> | <screen_name>`

选项可多选，例如：

```bash
tmd2 --user 12345 --user 67890 --list xxx --list xxx 
```

## Other

### 关于速率限制

Twitter API 限制一段时间内过快的请求 （例如每15分钟仅允许请求500次），当某一 API 请求次数将要达到这段时间内允许的上限，程序会打印一条信息后 Sleep 直到可用次数刷新。但这仅会阻塞尝试请求此 API 的 goroutine，所以后续可能有来自其余 goroutine 打印的内容迅速将这条 Sleep 通知覆盖 （程序是流水线式的工作流），让人认为程序莫名没有反应了

