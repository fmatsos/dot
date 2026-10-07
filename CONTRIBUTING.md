# Contributing to dot

Thanks for helping. For anything larger than a small fix, open an issue first so we can agree
on the direction before you spend time on it.

## Working on dot

Go, with [cobra](https://github.com/spf13/cobra). The [README](README.md#development) describes
the layout.

```sh
make test     # go vet and go test -race
make build    # the static binary ./dot
bash tests/<name>.sh   # one black-box test; they build and call the dot binary
```

- A command lives in its own file `cmd/dot/cmd_<name>.go` and registers itself with
  `func init() { register(newXxxCmd) }`; no shared file changes.
- Behavior comes with a test: a Go test for the logic, a `tests/<name>.sh` script for what the
  user sees. The tests run in a throw-away `HOME` and never call a real `bw`, `pass-cli` or `gh`.
- Keep real names, tokens and company references out of code, tests and commit messages: the
  CI runs the guard on files and commit metadata.
- Commit messages are short, in English, in the imperative ("Walk profile root folders on unlink").

## Unlicensing contributions

dot is in the public domain ([Unlicense](LICENSE)). To keep it free of anyone's copyright
monopoly, and to remove any doubt about the terms a contribution was made under, every
[non-trivial](https://www.gnu.org/prep/maintain/maintain.html#Legally-Significant) patch comes with
this statement, taken from the [Unlicense website](https://unlicense.org/#unlicensing-contributions):

> I dedicate any and all copyright interest in this software to the
> public domain. I make this dedication for the benefit of the public at
> large and to the detriment of my heirs and successors. I intend this
> dedication to be an overt act of relinquishment in perpetuity of all
> present and future rights to this software under copyright law.

You do not have to type it: the pull request template pre-fills it in the description of every
pull request, and **leaving it there is how you make the dedication**. If you open a pull request
another way (`gh pr create --body`, the API), paste the statement into its description yourself.
If you made the change as an employee of an organization, the statement may not be enough: your
employer has to disclaim its copyright too (see
[how SQLite handles it](https://www.sqlite.org/copyright.html)), so say so in the pull request.
