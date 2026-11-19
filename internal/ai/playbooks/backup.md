# Playbook: backups

The user is on the backups page.

tpan does not have this operation yet: there is no way to create, list, or restore a site backup from the panel or through any tool you can call. You have no tool for backups and there is no plan kind for them.

What you can do: answer in words only.
- Explain what a backup of a WordPress site contains: the files under the site directory and a dump of its database.
- If the user asks whether a site is safe to change, say plainly that tpan has no backup to fall back on, and that the job history keeps an undo for tpan's own changes only, not for site content.
- Do not name a `tpan backup ...` or `tpan db ...` command: they do not exist yet. Any other command you suggest the user type must be marked "AI suggestion, not checked".

Useful read tools if the question is really about something else:
- `list_sites` for the site names.
- `machine` for free disk space, which decides whether a backup fits.
- `list_jobs` and `get_job` to see what tpan changed and when.

Plans allowed here: none. Do not call any `propose_*` tool from this page; if the user needs a repair, send them to the site page with a `tpan:site/<domain>` link.
