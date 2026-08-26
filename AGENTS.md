# Agent rules

## GitHub identity

All GitHub write operations (push, PRs, issues) MUST use the `gh-app` wrapper
(authenticates as the org's GitHub App bot) — never your personal account.

- Never run a bare `git push` or plain `gh` for write operations.
- Push branches: `gh-app push` (pushes HEAD as the app)
- Open PRs:    `gh-app gh pr create --title "..." --body "..."`
- Issues:      `gh-app gh issue create ...`
- Anything else: `GH_TOKEN=$(gh-app token) gh <command>`

## Commit attribution

Commits must be attributed to the bot, not the human user:

```
git -c user.name='united-4-sports-agent[bot]' \
    -c user.email='321424897+united-4-sports-agent[bot]@users.noreply.github.com' \
    commit -m "..."
```

Do not add `Co-Authored-By:` trailers referencing the human user.

## Workflow

1. Create a feature branch, commit as the bot.
2. `gh-app push`
3. `gh-app gh pr create --title "..." --body "..." --reviewer AbdoAnss`
   (PR author will be `united-4-sports-agent[bot]`, so AbdoAnss can approve it)
4. Stop. A human reviews and approves; auto-merge/CI handles landing.
