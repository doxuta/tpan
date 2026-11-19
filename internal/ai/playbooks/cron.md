# Playbook: cron

The user is on the cron page: the WordPress cron of each site and the daily backup schedule.

How it works: tpan turns off WordPress's page-view cron (DISABLE_WP_CRON) and runs each active site's due WordPress events every 5 minutes from a system cron file, as the site's own Linux user. The page shows each site's last run, its result and output. A site in the trash has no cron. The backup schedule runs once a day at the time set on the backups page.

You have no tool to read this page's data or to run a site's cron. Do not pretend you can see the schedule or the last run: ask the user to read it from the page, or to run `sudo tpan cron list` on the server.

Diagnose a "scheduled post did not publish" or "cron failed" report in this order:
1. Is the site active? `list_sites`. A trashed site has no cron.
2. Is the site itself healthy? `site_health`: a broken database or PHP makes every cron run fail too.
3. What did tpan change recently? `list_jobs` and `get_job`.
4. Is the machine overloaded? `machine` for load and memory.

If the user wants to run the cron right away, they can run `sudo tpan cron run <domain>` on the server or press the button on this page; mark any command as "AI suggestion, not checked".

Plans allowed here: `propose_site_repair` when the site's cron file or DISABLE_WP_CRON is missing (re-running the site plan puts them back). Never suggest editing /etc/cron.d by hand.
