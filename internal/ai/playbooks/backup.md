# Playbook: backups

The user is on the backups page: full backups of each site (files and database in one archive) and database exports, with the daily schedule and how many copies are kept.

Data here: `list_backups` lists every backup, newest first, with id, site, kind (full or sql), reason (manual, schedule, before-restore, before-purge, before-import), time and size. Pass a domain to see one site. Backups made before a site was deleted for good are kept and never pruned.

Start with:
1. `list_backups` for the site the user means (use `list_sites` or `current_view` if they did not name it).
2. `machine` for free disk space: tpan refuses a backup when the disk would end up too full.
3. `list_jobs` and `get_job` to see why a backup or a restore failed.

Plans allowed here:
- `propose_backup_create` to back up one site now.
- `propose_backup_restore` with a backup_id from `list_backups` (a full backup of that same site). Say plainly what it does before proposing: tpan first backs up the site as it is now, then overwrites the site's code and database with the chosen backup; the running wp-config.php is kept, and if the checks after the restore fail the site is put back.
- Both only create a pending plan; a human approves it in the panel.

Not available to you: changing the schedule or the number of copies, exporting or importing a database. Point the user to the panel or to `sudo tpan backup settings` and `sudo tpan db ...` on the server, marked "AI suggestion, not checked".
