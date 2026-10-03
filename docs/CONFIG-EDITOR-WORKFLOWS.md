# Native configuration editor workflows

Issue #121 operator contract, 2026-10-03. Applies to the lightweight native
XKeen shell. XKeen code remains unchanged; the panel edits fixed data configs.

## Editing and switching modes

1. Open an existing config. **Form** and **Text** share the same working document.
   Text supports JSON/JSONC highlighting and explicit formatting; formatting
   preserves comments. Switching modes never saves or restarts anything.
2. Form edits preserve unknown fields. Unsupported fields remain available in
   Text. If Text is invalid, retain it and show the parse location; Form waits
   until syntax is valid. Do not replace an invalid draft with default values.
3. Undo/redo affects the working document only, including formatting and form
   edits. Switching files/modes retains drafts. Leaving with unsaved edits warns.
4. **Save draft** explicitly stores private unfinished work separately from native
   files; invalid syntax is permitted in a draft. Reopening offers Resume or
   Discard. No secret config is stored in browser localStorage or public logs.

## Saving and applying

5. **Save configuration** validates JSON/JSONC and the complete candidate config
   set with Xray before writing native files. Invalid candidates do not replace
   saved files. Show the affected config/line when the validator identifies it.
6. Saved files are **Awaiting restart**, distinct from unsaved working documents.
   List every changed config; subsequent saves retain the pre-apply snapshot,
   rather than replacing it with the last field edit. No restart on Save.
7. **Apply saved configurations** invokes native XKeen Restart once. All saved
   configs are read together. Unfinished drafts stay excluded. Show native output
   and independently inspect config identity, service and health afterward.
   HTTP acceptance/exit zero alone does not mark a generation verified.
8. Before Apply, **Discard saved changes** restores the pre-apply config set after
   checking that saved files have not changed externally; drafts are independent.
   No restart is needed when returning to the still-running generation.

## Failure and recovery

9. Validation/start failures retain the candidate and private diagnostic output.
   Show complete output within the documented bounded capture, with an explicit
   truncation indicator when exceeded; never silently claim complete logs.
   Offer Edit, Inspect console, or Restore previous configuration.
10. After Apply, retain one previous config generation. **Restore previous**
    validates it, saves it as a new pending change, and requires explicit Apply.
    Do not automatically roll back merely because the user closes a page or a
    health check fails. If failure is ambiguous, inspect instead of replaying.
11. An external edit triggers Reload/Compare. Never overwrite drift during Save
    or Restore. A panel restart retains explicit drafts/pending snapshot; lost
    runtime evidence means application state is unknown, not successful.
12. Logout clears editor/console memory. Raw configs and validator/native output
    are explicit authenticated private surfaces, with same-origin/CSRF guards.
    Paths are fixed IDs; no arbitrary filesystem or shell API is introduced.

## Acceptance scenarios

- Text → Form → Text preserves comments, unknown properties and large numbers;
  invalid Text remains editable; formatting/undo restores exact earlier bytes.
- A draft can be saved/reopened without changing native files or starting a job.
- Save DNS and routing separately: both appear pending; one Restart applies both.
- Invalid syntax or cross-file Xray validation leaves native files unchanged.
- Discard pending returns the full pre-apply generation, not the last single edit.
- Failed Restart shows private diagnostics and an optional validated restore;
  no automatic second Restart or forced rollback occurs.
- External drift blocks overwrite; logout/session replacement removes access to
  private buffers; interrupted Apply is inspected without replay.

Implementation status: source includes shared Form/Text documents, lazy
highlighted text editing, comment-preserving formatting, undo/redo, private drafts,
DNS resolvers, ordered routing rules, balancers and observatory forms. Saving a
set validates all native JSON/JSONC files together. Group Apply calls the fixed
native Restart once; discard and previous-generation restoration are explicit.
Process/config readback checks a new Xray PID/start identity, executable, sole
config directory and saved digest; this observation does not prove sustained
VPN/DNS health. The directory may come from an explicit `-confdir` argument or,
as stock XKeen launches Xray, `XRAY_LOCATION_CONFDIR` in the process environment.
Environment contents remain private; missing, duplicate or mismatched directory
values do not confirm Apply. Previous means pre-edit saved files, not a forensic copy of the
old process's loaded memory. Native start/restart cards use the same bookkeeping
when an editor set is pending. Routing, DNS and Components use one retained
workspace: navigation selects the relevant native file without discarding
working edits. View native console opens the existing job, never a new command.
The former appliance-policy Preview/Apply endpoints and brokers are retired.
No live editor qualification claimed.

The routing form additionally opens a lazy installed-geodata browser. Opening
the routing page alone does not read databases. It lists the regular native
`geosite*.dat` and `geoip*.dat` files, pages categories and contents, and searches
literal domain/IP membership using the encoded domain match types/CIDRs. Inverse
IP categories are labelled explicitly. This is database membership, not a claim
about the eventual first winning Xray rule or DNS resolution.

Each page is bound to a fresh file SHA256; a changed database invalidates paging.
Select an existing outbound or native balancer and place a category rule before
or after the existing rules. The operation edits the common working document
only, preserves its existing tokens/comments and can be undone. Save validates
the whole set; Apply still requires the explicit native restart. Destination
metadata projects outbound tags/protocols only, never node credentials.

Read bounds: 64 MiB/file, 16 MiB/entry buffer reused during scanning, 32 files,
8192 categories, 100 items/page, 256 KiB response and a 5-second HTTP deadline.
No persistent whole-file index is generated. The six installed database
snapshots passed local catalog/content/membership checks; router reader RSS and
live editor acceptance remain separate qualification.

Resource bounds: 2 MiB per text/draft, 8 MiB native candidate aggregate; one file
is fetched at a time. Private document JSON responses have a separate 32 MiB cap
to account for escaping; sanitized status responses retain their 512 KiB cap.
Validation output captures 256 KiB with explicit truncation. Drafts and pending
originals are explicit 0600 files in 0700 directories, excluded from public logs.
