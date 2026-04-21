# USB stick layout for `hwreport`

Copy the following four files to the **root** of a FAT32 or exFAT USB
stick:

```
<USB root>\
  autorun.inf
  hwreport.exe          <-- build with ..\build.ps1 and copy over
  run-report.ps1
  START HERE.cmd
```

## What the user sees

1. They plug in the stick.
   - Explorer shows the drive labelled **Hardware Report** with the
     hwreport icon.
   - Windows does **not** auto-run anything — on USB removable media
     AutoPlay has been locked down since Windows 7 / KB971029. The
     `autorun.inf` file is only used here to set the drive label and
     icon.
2. They open the drive and double-click **START HERE.cmd**.
3. A single PowerShell window appears. It runs `hwreport.exe` straight
   from the stick, writes the JSON to `\reports\<hostname>-<date>.json`
   on the stick, ejects the stick, and plays a two-tone beep to signal
   that it is safe to remove.
4. If something still holds a handle on the stick and the eject fails,
   the script plays a low single beep and asks the user to use "Safely
   Remove Hardware" before unplugging.

## Nothing is left on the reviewed computer

This was an explicit design goal. To achieve it:

- **`hwreport.exe` runs directly from the USB** — there is no staging
  to `%TEMP%`, no install, no service, no scheduled task.
- **`START HERE.cmd` only runs for a few milliseconds.** It uses
  `start` to launch a detached PowerShell and then exits, which
  releases `cmd.exe`'s handle on the `.cmd` file so the volume can be
  ejected later.
- **The PowerShell script is loaded via `Get-Content | Invoke-Expression`**
  rather than `-File`, so PowerShell never holds a persistent handle on
  `run-report.ps1`. Once read into memory, the file handle is closed.
- **The script immediately relocates away from the USB** using both
  `Set-Location` and `[Environment]::CurrentDirectory`, so the
  PowerShell process doesn't keep an implicit handle on the USB root
  directory.
- **`hwreport.exe` is launched with a working directory of
  `%SystemRoot%`** so its process does not pin the USB either. When it
  exits, its image-section handle is released, at which point the USB
  has no open handles from any process and can be ejected.

The net effect: the only thing that ever "touched" `C:` is the in-memory
PowerShell process, using the `powershell.exe` that was already on the
box.

## Collecting the reports afterwards

Once you have several sticks' worth of `reports\*.json`, feed the files
to `hwoverview.exe --in <folder>` to get the aggregated HTML overview.
