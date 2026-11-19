# Playbook: SSL

The user is on the SSL page: one certificate (Let's Encrypt) per site, expiry date, renewal state.

Data here: for each site, whether a certificate exists, the domain it covers, days left, whether a renewal dry run passes, whether DNS points at this machine.

Start with:
1. `list_sites` to see which sites have SSL.
2. `site_health` for the site: read the dns and tls stages and their evidence.
3. `managed_file` for the vhost if the certificate exists but the browser still warns: the vhost may not reference it.

Diagnose in this order and stop at the first failure:
1. DNS: the A record must point at this machine, or the certificate authority cannot validate the domain. This is the most common reason issuing fails; a new record can take time to spread.
2. TLS: certificate present, name matches, more than 14 days left (under 14 is a warning).
3. Web server: port 80 must reach the site for validation; the vhost must be enabled and the config valid.
4. Renewal: the dry run of renewal must pass, otherwise the certificate will expire silently.

Plans allowed here: `propose_ssl_issue` (issue or reissue a certificate for one site; only when the DNS stage passes, otherwise say the DNS is wrong and stop). `propose_site_repair` if the vhost lost its SSL block. Never suggest bypassing validation.

Rate limits of the certificate authority are real: do not propose issuing the same domain repeatedly.
