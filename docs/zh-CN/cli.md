# 命令行

> 本文译自 [cli.md](../cli.md)，对应提交 `608d812`。译文与英文版不一致时，以英文版为准。本译文尚未经过母语者审校，欢迎提交修正。

不带命令运行 `tele` 会打开一个窗口。所有命令及其参数见 [commands.md](commands.md)。本页说明所有命令共同遵守的约定：如何命名、以什么退出码结束、JSON 输出是什么样子。

<a id="a-command-needs-a-tele-to-ask"></a>
## 命令需要一个可以询问的 `tele`

命令会询问持有账号的 `tele`（你打开的某个窗口，或者服务），自己从不启动 `tele`。如果没有正在运行的 `tele`，命令以退出码 3 结束，并提示如何获得一个：打开 `tele`，或者用 `tele daemon install` 安装服务。因此脚本适合配合服务使用：无论有没有打开窗口，服务从登录系统起就在运行。参见[无窗口运行](configuration.md#running-without-a-window)。

正在运行的命令不算有人在看这个账号：它不会阻止桌面通知的显示，关闭窗口时也不会把它算作会一并关闭的窗口。

作用于账号的命令会等待账号就绪：持有账号的 `tele` 正在连接、加载聊天列表，或者在退出登录后启动下一个账号时，命令都会等待，最多 30 秒。如果账号在等待有人登录，命令会立即以退出码 4 结束：登录要在窗口中完成。

<a id="naming-a-chat"></a>
## 如何指定聊天

命令用以下三种方式之一指定聊天，在所有命令中都相同：

| 名称        | 指的是哪个聊天                                                                 |
| ----------- | ------------------------------------------------------------------------------ |
| `42`        | 具有这个 id 的聊天，即 `tele chats` 打印出的 id                                |
| `@username` | 具有这个 username 的用户、机器人或频道，不区分大小写。不在你的聊天中的会向 Telegram 查询 |
| `me`        | 你自己的聊天，即收藏夹                                                         |

标题不能用来指定聊天：标题会变，两个聊天也可能同名。其他任何写法都以退出码 2 结束，指不到任何聊天的名称以退出码 5 结束。

<a id="how-commands-are-named"></a>
## 命令的命名规则

1. 顶层命令作用于 Telegram；`daemon` 子命令作用于持有账号的 `tele`。
2. 命令都是完整的单词：没有 `tele ls`，也没有 `tele d`。
3. 长参数使用两个短横线，如 `--name`。只有需要手动输入的参数才有短形式：`-c` 对应 `--config`，`-h` 对应 `--help`，`-e` 对应 `--debug`。
4. 执行难以撤销操作的参数没有短形式。
5. 面向程序的输出一律用 `--json`，只要命令有这种输出。
6. 同一含义在所有命令中使用同一名称，例如 `--limit`。
7. 所有命令以相同方式指定聊天。
8. 动作是命令，参数只用来调整命令。所以是 `tele version` 和 `tele theme check`，而不是 `--version` 和 `--theme-check`。这两个参数以及 `--theme-dump` 仍然有效，供调用它们的包管理器使用，但不在帮助中列出。

<a id="exit-codes"></a>
## 退出码

| 代码 | 含义                                                                            |
| ---- | ------------------------------------------------------------------------------- |
| 0    | 完成                                                                            |
| 1    | 其他任何错误；具体原因输出在 stderr                                             |
| 2    | 无法解析命令行：未知的参数、命令或实参                                          |
| 3    | 没有持有账号、可供询问的 `tele`                                                 |
| 4    | 账号未登录                                                                      |
| 5    | 没有这个聊天、话题或文件夹                                                      |
| 6    | 消息无法发送，重试也不会改变结果                                                |
| 75   | 消息仍在队列中时放弃等待，或者 Telegram 要求等待；稍后可能会成功                |

6 和 75 留给发送消息，这项功能会在之后的版本中推出。届时脚本可以区分 75（值得稍后重试）和 6（不值得）。

<a id="json"></a>
## JSON

- `--json` 是唯一面向程序的格式。需要 YAML 时，用 `| yq -P`。
- 回答单个结果的命令输出一个对象。回答列表的命令输出 JSON Lines：每行一个对象，在顺序代表时间的地方按从旧到新排列。
- 字段名使用 `snake_case`。时间使用 RFC 3339 格式。
- 不适用的字段直接省略，而不是输出零或 null。脚本应把缺失的字段理解为“不存在”，而不是“零”。
- 本页描述的内容在同一个主版本内保持不变：可能新增字段，但在 tele 3 之前不会重命名或删除任何字段。
- 命令失败时 stdout 保持为空：原因由退出码和 stderr 说明。

<a id="tele-chats---json"></a>
## `tele chats --json`

```json
{"id":42,"title":"Bob","username":"bob","kind":"user","unread":3,"mentions":1,"reactions":0,"unread_mark":false,"muted":false,"archived":false,"forum":false}
{"id":300,"title":"Gophers","kind":"group","unread":1,"mentions":0,"reactions":2,"unread_mark":true,"muted":true,"archived":false,"forum":true}
```

| 字段          | 含义                                                                                   |
| ------------- | -------------------------------------------------------------------------------------- |
| `id`          | 在所有命令中用来指定这个聊天                                                           |
| `title`       | 聊天的标题，或对方的名字                                                               |
| `username`    | 公开的 username，不带 `@`；没有时省略                                                  |
| `kind`        | `user`、`bot`、`group` 或 `channel`                                                    |
| `unread`      | 未读消息数；论坛则是有未读的话题数                                                     |
| `mentions`    | 提及你的未读消息数                                                                     |
| `reactions`   | 你还没看到的、对你的消息的回应数                                                       |
| `unread_mark` | 是否被手动标为未读                                                                     |
| `muted`       | 是否已静音                                                                             |
| `archived`    | 是否已归档                                                                             |
| `forum`       | 是否是论坛，即消息都在话题里                                                           |

<a id="tele-topics---json"></a>
## `tele topics --json`

```json
{"id":17,"title":"Releases","unread":4,"mentions":1,"reactions":0,"muted":true,"pinned":false,"closed":false}
```

| 字段        | 含义                                                               |
| ----------- | ------------------------------------------------------------------ |
| `id`        | `tele show --topic` 接受的 id                                      |
| `title`     | 话题的标题                                                         |
| `unread`、`mentions`、`reactions` | 与聊天相同，只统计这个话题                     |
| `muted`     | 话题是否已静音，无论是它自己的设置还是论坛的设置                   |
| `pinned`    | 是否置顶在列表上方                                                 |
| `closed`    | 是否只有管理员能在其中发言                                         |

<a id="tele-show---json"></a>
## `tele show --json`

```json
{"id":512,"date":"2026-10-25T09:12:00+03:00","sender_id":77,"sender":"Ada Lovelace","out":true,"text":"morning!","reply_to":511}
{"id":513,"date":"2026-10-25T09:13:00+03:00","sender_id":42,"sender":"Bob","out":false,"media":{"kind":"voice","duration":12}}
```

| 字段        | 含义                                                                                        |
| ----------- | ------------------------------------------------------------------------------------------- |
| `id`        | 消息的 id，`--before` 接受的就是它                                                          |
| `date`      | 发送时间                                                                                    |
| `sender_id`、`sender` | 发送者的 id 和名字；你自己的消息带你的 id 和名字。在频道中是频道本身              |
| `out`       | 是否是你发送的                                                                              |
| `text`      | 文本，或媒体消息的说明文字；没有时省略                                                      |
| `reply_to`  | 它所回复的消息的 id；不是回复时省略                                                         |
| `media`     | 消息带的媒体，没有时省略：`kind`（`photo`、`video`、`round_video`、`voice`、`audio`、`sticker`、`gif`、`file`、`location` 或 `other`）以及媒体自身提供的信息：`duration`（秒）、`file_name`、`size`（字节）、`title`、`performer`、`emoji`。从不给出文件所在的位置：什么都不会下载 |

<a id="tele-daemon-status---json"></a>
## `tele daemon status --json`

```json
{
  "state_dir": "/home/ada/.local/state/tele",
  "host": {"binary": "2.1.0", "protocol": 6, "pid": 4242, "kept": "service", "config": "/home/ada/.config/tele/config.yml"},
  "since": "2026-10-21T09:00:00Z",
  "windows": 1,
  "commands": 0,
  "account": {
    "phase": "ready",
    "user": "ada",
    "link": "up",
    "link_since": "2026-10-21T09:00:04Z",
    "last_update": "2026-10-21T12:41:10Z",
    "outbox": 0,
    "throttle": {"method": "messages.getHistory", "until": "2026-10-21T12:41:42Z"}
  },
  "this": {"binary": "2.1.0", "protocol": 6},
  "service_warning": "The service runs /opt/homebrew/bin/tele, which is not there any more."
}
```

| 字段               | 含义                                                                                           |
| ------------------ | ---------------------------------------------------------------------------------------------- |
| `state_dir`        | 被询问的状态目录                                                                               |
| `host`             | 持有它的 `tele`：版本、协议、进程 id、配置文件，以及 `kept`：`window`、`daemon` 或 `service`   |
| `since`            | 该 `tele` 的启动时间                                                                           |
| `windows`          | 连接到它的窗口数                                                                               |
| `commands`         | 此刻正在询问它的其他命令数                                                                     |
| `account`          | 账号；在前一个账号结束、下一个账号开始之前省略                                                 |
| `account.phase`    | `connecting`、`logging_in`、`loading` 或 `ready`                                               |
| `account.user`     | 已登录用户的用户名；Telegram 尚未告知或用户没有用户名时省略                                    |
| `account.ended`    | 连接以什么结束；连接运行期间省略                                                               |
| `account.link`     | `up`、`down` 或 `unknown`，从何时起由 `link_since` 给出                                        |
| `account.last_update` | 最近一次收到 Telegram 更新的时间                                                            |
| `account.outbox`   | 等待发送的消息数                                                                               |
| `account.throttle` | Telegram 要求的、正在阻塞某项操作的等待，以及持续到何时；没有等待时省略                        |
| `this`             | 发起询问的 `tele`：它的版本和协议                                                              |
| `service_warning`  | 已安装的服务有什么问题；没有问题时省略                                                         |

使用其他协议版本的 `tele` 只会被点名：只有 `host` 和 `this`，不包含任何没被问到的内容。如果没有 `tele` 持有该状态目录，则不输出任何内容，退出码为 3。
