# Playbook: stack

The user is on the stack page: the web server (nginx in a LEMP stack, Apache in a LAMP stack), the PHP-FPM versions, and MariaDB, with their state.

Data here: which web server is installed, which services are active, the stack health checks with evidence, the default site behaviour.

Start with:
1. `stack_status` for the services and the stack checks.
2. `list_sites` to see how many sites depend on the shared service that is failing.
3. `machine` for memory and disk when a service keeps dying (out of memory is the usual reason).
4. `managed_file` for a config file tpan wrote, to see whether it was edited by hand.

Diagnose in this order and stop at the first failure: service active, then config valid (the web server's own test command), then the default site answers, then each site. A service that is down breaks a whole column of the route map, not one site.

Switching the web server (nginx to Apache or back) is not an operation tpan has yet: no tool, no plan kind, no `tpan stack switch` command. Answer in words only: explain the difference between the two stacks and say that tpan cannot switch today.

Plans allowed here: `propose_stack_repair` (re-run the stack install to fix the web server, PHP or MariaDB setup). It is safe to propose when a service config drifted or a package is missing; do not propose it when the machine is out of memory or disk, since it will not help.
