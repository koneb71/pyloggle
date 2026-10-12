# Help and shortcuts

The **Help** page, at the bottom of the sidebar (`g` then `h`), gathers what you need while
using Pyloggle:

- links to the main guides of this documentation;
- **Connect your app**, with this server's address filled in;
- search examples for Logs and Requests; click one to try it;
- every keyboard shortcut;
- the words the app uses, what each means, and what engineers call it;
- this server's version and how people sign in, with links to report a problem and to the
  open-source licenses.

![The Help page: guides, Connect your app, and search examples](../images/screenshots/help-light.png#gh-light-mode-only)
![The Help page: guides, Connect your app, and search examples](../images/screenshots/help-dark.png#gh-dark-mode-only)

Pages with more to explain have a **Learn more** link to the matching page of this
documentation. A server can point those links at an internal copy of the docs, or hide them,
with `PYLOGGLE_DOCS_URL`.

## Keyboard shortcuts

Press `?` on any page to see them.

| Keys | What they do |
|---|---|
| `g` then `o` | Go to Overview |
| `g` then `i` | Go to Issues |
| `g` then `l` | Go to Logs |
| `g` then `t` | Go to Requests |
| `g` then `d` | Go to Dashboards |
| `g` then `a` | Go to Alerts |
| `g` then `p` | Go to Projects |
| `g` then `s` | Go to System (administrators) |
| `g` then `h` | Go to Help |
| `/` | Search, on Logs and Requests |
| `j` / `k` | Next and previous message, on Logs |
| `Esc` | Close a panel or dialog |
| `?` | Show the shortcuts |

## Looks and screen sizes

The app follows your device's light or dark setting; the sun and moon button at the bottom of
the sidebar (of the menu, on a phone) overrides it.

Every page works from a desk to a phone:

![Overview, an issue and Logs on a phone: a top bar with the menu button, cards in one column, and messages in two-line rows](../images/screenshots/phone-light.png#gh-light-mode-only)
![Overview, an issue and Logs on a phone: a top bar with the menu button, cards in one column, and messages in two-line rows](../images/screenshots/phone-dark.png#gh-dark-mode-only)

- **Wide screens** show the sidebar with page names. Below 1200 pixels wide it shrinks to icons,
  and the button at its bottom expands or collapses it.
- **Phones and small tablets** (below 768 pixels) get a bar at the top with the current project.
  Its menu button opens the pages, the project switcher and your account.
- **Lists** that are tables on a desk become two-line rows where there isn't room. A message
  shows its time, severity and app above its text, and each step of a request shows its name
  above its bar. Columns that matter less step out; the important ones, such as how many people
  an issue affected, move into the row.
- **Details** of a message or a request step cover the screen. On Logs, the **Filters** button
  opens the filters, and **Show … messages** closes them.
- **Touch screens** get slightly larger buttons and fields. The actions a mouse finds by
  hovering, such as filtering by a value, hiding it or copying it, are always shown.
