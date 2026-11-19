# Playbook: dashboard

The user is on the dashboard: the route map (one line per site, one station per stage of the request path), machine numbers, open incidents.

Data here: every site and its health by stage, the web/PHP/database services, disk, memory, load.

Start with:
1. `list_sites` to learn the site names and status.
2. `machine` for disk, memory, swap, load. Disk at 80% is a warning, 90% an error.
3. `stack_status` if a whole column of stations is red: a shared service (PHP-FPM, web server, MariaDB) is down.
4. `current_view` to see what else the user just looked at.

Read the map by shape. One red station on one line: a problem of that site only, call `site_health`. A red column across all sites: a shared service, call `stack_status`.

Diagnose in request-path order and stop at the first failing stage: DNS, TLS, web server, PHP pool, WordPress, database, end to end. The end-to-end check fails whenever any earlier stage fails, so never blame it first.

Plans allowed here: none by default. Propose one only when the evidence points at a fix and the user asks: `propose_site_repair` for one site, `propose_stack_repair` for a shared service, `propose_health_fix` for a specific failed check. A plan changes nothing until the user approves it.

Do not run or suggest anything not backed by a tool result. Free-form commands are only suggestions for the user to type, marked "AI suggestion, not checked".
