# 快速上手

> 本文译自 [getting-started.md](../getting-started.md)，对应提交 `0eb84d9`。译文与英文版不一致时，以英文版为准。本译文尚未经过母语者审校，欢迎提交修正。

第一次运行 `tele` 时会发生什么，以及从 1.11 升级时有哪些变化。安装方法见 [README](../../README.zh-CN.md#installation)。

<a id="first-launch"></a>
## 首次启动

```sh
tele
```

首次运行时，`tele` 会创建 `~/.config/tele/config.yml`，然后依次询问：

- 你的手机号
- Telegram 发给你的验证码
- 两步验证密码（如果设置了）

<a id="running-as-a-service"></a>
## 作为服务运行

登录后，`tele` 会询问一次是否作为服务在后台持续运行。回答 `y`，关闭窗口后账号仍然在线，通知照常到达。回答 `n` 或按 Esc，`tele` 只在窗口打开时运行。

之后可以用 `tele daemon install` 或 `tele daemon uninstall` 改变主意。在无法安装服务的地方，或者服务已经安装时，`tele` 不会询问。服务在各个系统上如何运行：[configuration.md](configuration.md#running-without-a-window)。

<a id="app-key"></a>
## 应用密钥

`tele` 通过应用密钥连接 Telegram，它标识的是应用程序，而不是你本人。每个构建都自带一个，所以登录时你不需要做任何事。

如果 Telegram 拒绝了这个密钥，`tele` 会提示 `app key blocked by Telegram`。从源码编译的构建可能遇到这种情况，因为它们共用同一个密钥。[app-key.md](app-key.md) 介绍了两种解决办法。

<a id="upgrading-from-111"></a>
## 从 1.11 升级

- 在已有 `tele` 运行时再打开一个，不会再报错：第二个会作为同一账号的另一个窗口连接到第一个。
- 登录后，`tele` 会询问一次是否作为服务运行（见上文）。
- 参数现在使用两个短横线（`--config`、`--debug`）；短参数 `-e` 仍然可用。版本号和主题工具改成了命令：`tele version`、`tele theme check`、`tele theme dump`。
- 1.11 和 2.0 不能同时使用同一个账号：启动 2.0 前请先退出 1.11。

完整的变更记录（英文）：[CHANGELOG.md](../../CHANGELOG.md)
