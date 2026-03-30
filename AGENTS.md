# AGENTS.md

## Project Rule: Keep Specs In Sync

Any implementation change in this repository must be reflected in the project documentation and agent working files in the same iteration.

This includes, when applicable:

- `requirements.md`
- `decisions.md`
- `artifacts/01_technical_spec.md`
- `artifacts/02_agent_debate.md`
- `artifacts/03_implementation_plan.md`
- `artifacts/04_qa_checklist.md`

## Expected Behavior For Agents

- Do not treat specs as optional follow-up work.
- If UI, workflow, architecture, security posture, deployment behavior, branding, routes, auth roadmap, or integration behavior changes, update the corresponding spec files before closing the task.
- If a change only affects implementation details and does not belong in the functional spec, record it at least in the most relevant artifact or decision log.
- If derived artifacts become outdated because of a confirmed change, refresh them in the same task so they remain consistent with `requirements.md` and `decisions.md`.
- Commit at every meaningful step so rollback stays simple.
- Prefer small, sequential commits over large grouped commits.
- Include both implementation and documentation/spec synchronization in the same commit unless the user explicitly asks to separate them.
- Before making a new change, check whether there are pending uncommitted changes from the previous step and commit them first when feasible.
- Never commit secrets, tokens, passwords, or private environment values into tracked config files.
- `app/config/config.json` is local-only and must remain ignored by git.
- `Makefile.deploy.local` is local-only and must remain ignored by git unless the user explicitly asks to start versioning it.
- Changes to shared config shape must be reflected in `app/config/config.example.json`, not by committing local runtime values.
- If a release version changes, update `VERSION` and any user-visible version label in the UI in the same task so they stay aligned.
- For a release request, prefer this order: commit pending releaseable changes, bump `VERSION`, create an annotated tag `vX.Y.Z`, push `main`, push the tag, then create the GitHub release.
- If `gh release create` fails for missing GitHub scopes, report that explicitly instead of assuming the release exists.

## Current Documentation Intent

- `requirements.md`: source description of product and technical scope.
- `decisions.md`: confirmed human decisions and active constraints.
- `artifacts/01_technical_spec.md`: consolidated technical spec.
- `artifacts/02_agent_debate.md`: design review and decision framing.
- `artifacts/03_implementation_plan.md`: milestone plan.
- `artifacts/04_qa_checklist.md`: readiness and QA checklist.

## Practical Rule Of Thumb

If a future reader could be surprised because the code says one thing and the specs say another, update the specs before finishing.
