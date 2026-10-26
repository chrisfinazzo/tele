# Command line

`tele` with no command opens a window. Every command and its flags are in
[commands.md](commands.md); this page is the contract they keep: how they are
named, what they exit with, and what their JSON looks like.

## A command needs a `tele` to ask

A command asks the `tele` that holds the account (a window you have open, or
the service) and never starts one of its own. With none running it exits 3
and says how to get one: open `tele`, or install the service with
`tele daemon install`. Scripting therefore wants the service: it is there from
login onwards, whether or not a window is open. See
[Running without a window](configuration.md#running-without-a-window).

A command running is not someone looking at the account: it does not stop a
desktop notification from being shown, and quitting a window does not count
it among the windows it would close.

A command that acts on the account waits for it to be ready: while the `tele`
holding it connects, loads the chat list, or starts the next account after a
log out. It waits 30 seconds at most. An account waiting for someone to log in
exits 4 at once: logging in is done in a window.

## Naming a chat

A command names a chat in one of three ways, the same in every command:

| Name        | Which chat                                                                     |
| ----------- | ------------------------------------------------------------------------------ |
| `42`        | The chat with that id, as `tele chats` prints it                               |
| `@username` | The user, bot or channel with that username, in any case. One that is not among your chats is asked of Telegram |
| `me`        | Your own chat, Saved Messages                                                  |

A title names nothing: titles change, and two chats can share one. Anything
else exits 2, and a name that is no chat exits 5.

## How commands are named

1. The top level acts on Telegram; the `daemon` subtree acts on the `tele`
   that holds the account.
2. Commands are whole words: no `tele ls`, no `tele d`.
3. Long flags take two dashes, `--name`. A short form exists only for what is
   typed by hand: `-c` for `--config`, `-h` for `--help`, `-e` for `--debug`.
4. A flag that does something hard to undo has no short form.
5. Machine output is `--json`, in every command that has any.
6. The same meaning has the same name in every command, such as `--limit`.
7. A chat is named the same way in every command.
8. An action is a command; a flag only shapes one. That is why it is
   `tele version` and `tele theme check`, not `--version` and `--theme-check`.
   Those two flags, and `--theme-dump`, still answer for the package managers
   that run them, but are not offered.

## Exit codes

| Code | Meaning                                                                         |
| ---- | ------------------------------------------------------------------------------- |
| 0    | Done                                                                            |
| 1    | Anything else went wrong; what it was is said on stderr                         |
| 2    | The command line could not be read: an unknown flag, command or argument        |
| 3    | No `tele` holds the account to ask                                              |
| 4    | The account is not logged in                                                    |
| 5    | No such chat, topic or folder                                                   |
| 6    | A message could not be sent, and trying again will not change that              |
| 75   | Gave up waiting while a message was still queued, or Telegram asked to wait; it may work later |

6 and 75 are reserved for sending messages, which comes in a later release. A
script will be able to tell 75, worth trying again later, from 6, which is
not.

## JSON

- `--json` is the only machine format. For YAML, `| yq -P`.
- A command answering with one thing prints one object. One answering with a
  list prints JSON Lines: an object per line, oldest first where order means
  time.
- Field names are `snake_case`. Times are RFC 3339.
- A field that does not apply is left out rather than given as zero or null.
  A script reads a missing field as "not there", never as "zero".
- What is described here holds within a major version: fields may be added,
  none is renamed or removed before tele 3.
- When a command fails, stdout stays empty: the exit code and stderr say why.

## `tele chats --json`

```json
{"id":42,"title":"Bob","username":"bob","kind":"user","unread":3,"mentions":1,"reactions":0,"unread_mark":false,"muted":false,"archived":false,"forum":false}
{"id":300,"title":"Gophers","kind":"group","unread":1,"mentions":0,"reactions":2,"unread_mark":true,"muted":true,"archived":false,"forum":true}
```

| Field         | What it is                                                                             |
| ------------- | -------------------------------------------------------------------------------------- |
| `id`          | What names the chat in every command                                                   |
| `title`       | The chat's title, or the person's name                                                 |
| `username`    | Its public username, without the `@`; left out when it has none                        |
| `kind`        | `user`, `bot`, `group` or `channel`                                                    |
| `unread`      | How many messages are unread; for a forum, how many of its topics have unread          |
| `mentions`    | How many unread messages mention you                                                   |
| `reactions`   | How many reactions to your messages you have not seen                                  |
| `unread_mark` | Whether the chat is marked unread by hand                                              |
| `muted`       | Whether its notifications are off                                                      |
| `archived`    | Whether it is in the archive                                                           |
| `forum`       | Whether it is a forum, whose messages are in its topics                                |

## `tele topics --json`

```json
{"id":17,"title":"Releases","unread":4,"mentions":1,"reactions":0,"muted":true,"pinned":false,"closed":false}
```

| Field       | What it is                                                         |
| ----------- | ------------------------------------------------------------------ |
| `id`        | What `tele show --topic` takes                                     |
| `title`     | The topic's title                                                  |
| `unread`, `mentions`, `reactions` | As for a chat, counted in this topic         |
| `muted`     | Whether the topic is muted, by its own setting or its forum's      |
| `pinned`    | Whether it is pinned to the top of the list                        |
| `closed`    | Whether only admins may write in it                                |

## `tele show --json`

```json
{"id":512,"date":"2026-10-25T09:12:00+03:00","sender_id":77,"sender":"Ada Lovelace","out":true,"text":"morning!","reply_to":511}
{"id":513,"date":"2026-10-25T09:13:00+03:00","sender_id":42,"sender":"Bob","out":false,"media":{"kind":"voice","duration":12}}
```

| Field       | What it is                                                                                  |
| ----------- | ------------------------------------------------------------------------------------------- |
| `id`        | The message's id, what `--before` takes                                                     |
| `date`      | When it was sent                                                                            |
| `sender_id`, `sender` | Who sent it, by id and by name; your own messages carry yours. In a channel, the channel |
| `out`       | Whether you sent it                                                                         |
| `text`      | Its text, or a media message's caption; left out when there is none                         |
| `reply_to`  | The id of the message it answers; left out when it answers none                             |
| `media`     | What media it carries, left out when it carries none: `kind` (`photo`, `video`, `round_video`, `voice`, `audio`, `sticker`, `gif`, `file`, `location` or `other`) and what the media says about itself: `duration` in seconds, `file_name`, `size` in bytes, `title`, `performer`, `emoji`. Never where a file is: nothing is downloaded |

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

| Field              | What it is                                                                                     |
| ------------------ | ---------------------------------------------------------------------------------------------- |
| `state_dir`        | The state directory asked about                                                                |
| `host`             | The `tele` that holds it: its version, protocol, process id, config file, and `kept`: `window`, `daemon` or `service` |
| `since`            | When that `tele` started                                                                       |
| `windows`          | How many windows are joined to it                                                              |
| `commands`         | How many other commands are asking it right now                                                |
| `account`          | The account, left out between one and the next                                                 |
| `account.phase`    | `connecting`, `logging_in`, `loading` or `ready`                                               |
| `account.user`     | The logged-in user's username, left out until Telegram has said or when there is none         |
| `account.ended`    | What the connection ended with, left out while it runs                                         |
| `account.link`     | `up`, `down` or `unknown`, and `link_since` since when                                         |
| `account.last_update` | When the last update from Telegram arrived                                                  |
| `account.outbox`   | How many messages are waiting to be sent                                                       |
| `account.throttle` | A wait Telegram asked for that something is held by, and until when; left out when there is none |
| `this`             | The `tele` that asked: its version and protocol                                                |
| `service_warning`  | What is wrong with the installed service, left out when nothing is                             |

A `tele` speaking another protocol is only named: `host` and `this` are there,
and nothing it was not asked. With no `tele` holding the state directory,
nothing is printed and the exit code is 3.
