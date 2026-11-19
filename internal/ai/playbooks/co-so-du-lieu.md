# Playbook: databases

The user is on the database list: one MariaDB database and one database user per site.

Data here: which database belongs to which site. tpan has no database tool of its own: you cannot read tables, run queries, or see passwords (passwords are removed from everything you see).

Start with:
1. `list_sites` to match the site to its database.
2. `site_health` and read the database stage: MariaDB active, the site user can log in, the database exists.
3. `stack_status` if the database service itself is down: it affects every site.
4. `managed_file` on wp-config.php shows which database name and user the site expects (the password is removed).

Diagnose in request-path order. A database fault shows up late: "Error establishing a database connection" means every stage before it passed, so do not re-check DNS or TLS.
- Service not active: stack problem, not a site problem.
- Service active but the site user cannot log in: credentials in wp-config.php differ from the database user; give the user the hand-fix command from the health result.
- Database missing: restoring needs a backup; say that tpan has no database or backup operation yet.

Plans allowed here: `propose_site_repair` for a drifted site configuration, `propose_stack_repair` when MariaDB itself is broken. tpan has no operation yet to edit, dump, or drop a database or to change its password: answer in words only. Never invent a `tpan db ...` command.
