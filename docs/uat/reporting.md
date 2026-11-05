# UAT — call detail reporting (D-88)

1. Sign in as a tenant administrator, place a call from an extension and
   let it end.
2. Open **Reports**, set the date range to today and the extension to the
   caller. Expect the terminated call once, with duration, outcome and
   the caller's extension/person.
3. Click **Export CSV** and open the file. Expect a header and one row
   per participant, with `extension_id`/`principal_id` filled for known
   parties and no `PJSIP/` or channel value anywhere.
4. Sign in as an agent who was not on that call. Open **Reports**; the
   call must not appear, even if the agent filters by the other person.
5. Check the audit log (or the identity audit API) for exactly one
   `report.export` entry naming the exporting person and row count.
6. An in-progress call must not appear in Reports; it remains in Call
   history.
