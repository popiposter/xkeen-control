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

Implementation status: source now includes shared Form/Text documents, lazy
highlighted text editing, comment-preserving formatting, undo/redo, private draft
storage, full candidate validation/diagnostics and durable pending file tracking
with first-save originals. Group Apply, pre-apply discard and post-apply previous
generation restore remain in progress before editor delivery. No live editor
qualification claimed.

Resource bounds: 2 MiB per text/draft, 8 MiB native candidate aggregate; one file
is fetched at a time. Private document JSON responses have a separate 32 MiB cap
to account for escaping; sanitized status responses retain their 512 KiB cap.
Validation output captures 256 KiB with explicit truncation. Drafts and pending
originals are explicit 0600 files in 0700 directories, excluded from public logs.
