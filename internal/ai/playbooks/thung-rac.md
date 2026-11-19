# Playbook: trash

The user is on the trash page: sites that were removed and kept for a while before being purged.

tpan does not have this operation yet: there is no trash to list, restore from, or purge, you have no tool for it, and there is no plan kind for it. Removing a site with `propose_site_remove` deletes it for good. You cannot see anything that was removed.

What you can do: answer in words only.
- Say plainly that a removed site cannot be brought back by tpan today, and that this is why removal needs a very careful read.
- Do not name a `tpan trash ...` or `tpan site restore ...` command: they do not exist yet.
- If the user wants space back, show the numbers first with `machine` and let them decide.

Useful read tools:
- `list_sites` shows only live sites: a site missing from it was removed or never existed.
- `list_jobs` and `get_job` show when and by whom a site was removed.
- `machine` for disk space.

Plans allowed here: none. `propose_site_remove` deletes a live site; do not offer it from this page.
