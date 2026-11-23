#Requires -Version 3.0
<#
.SYNOPSIS
Creates the hwreport drop share, the Workstations OU and two unlinked GPOs:
"WinRM - LAN admins" and "hwreport inventory".

.DESCRIPTION
Run on a domain controller as a Domain Admin (Windows PowerShell 3.0+, with
the ActiveDirectory and GroupPolicy modules). All AD and SYSVOL writes go to
the DC the script runs on; normal replication carries them to the other DCs.

The script never links a GPO. Link them yourself, for example:
  New-GPLink -Name 'WinRM - LAN admins' -Target 'OU=Workstations,DC=CosmoJPN,DC=local'

Share  (\\<this DC>\<ShareName>, folder <ShareRoot>)
  - root:     Domain Users read & execute, so hwreportw.exe can run from it
  - reports\: Domain Users may create files but not list or read them; each
              file's creator may write it. Admins have full control.

GPO "WinRM - LAN admins"
  - Allow remote server management through WinRM (listen on all addresses)
  - Firewall: TCP 5985 inbound, Domain profile, remote addresses <Subnet> only
  - WinRM service: Automatic and started (Group Policy Preferences)
  Over the network Windows filters local accounts out of the Administrators
  token, so in practice only domain accounts that are local admins get in.

GPO "hwreport inventory"
  - Scheduled task (Group Policy Preferences, "remove when no longer applied")
    that runs as BUILTIN\Users in each signed-in user's session, least
    privilege. Triggers 5 minutes after logon and monthly on the 1st at 11:00
    (for people who never sign out). It runs
    hwreportw.exe --out <share>\reports --once <OnceKey>, so each PC reports once.

Running the script again leaves the share and OU alone, and it refuses to
overwrite an existing GPO with the same name.
#>
[CmdletBinding()]
param(
    [string]$ShareRoot = 'E:\Shares\hwreport',
    [string]$ShareName = 'hwreport$',
    [string]$Subnet    = '192.168.100.0/255.255.255.0',
    [string]$OnceKey   = 'inventory-2026-11',
    [string]$WinRMGpoName    = 'WinRM - LAN admins',
    [string]$InventoryGpoName = 'hwreport inventory'
)

$ErrorActionPreference = 'Stop'
Import-Module ActiveDirectory, GroupPolicy

$dc      = [System.Net.Dns]::GetHostEntry($env:COMPUTERNAME).HostName
$domain  = Get-ADDomain -Server $dc
$sidOf   = { param($rid) New-Object System.Security.Principal.SecurityIdentifier ("{0}-{1}" -f $domain.DomainSID.Value, $rid) }
$sidSystem       = New-Object System.Security.Principal.SecurityIdentifier 'S-1-5-18'
$sidAdmins       = New-Object System.Security.Principal.SecurityIdentifier 'S-1-5-32-544'
$sidCreatorOwner = New-Object System.Security.Principal.SecurityIdentifier 'S-1-3-0'
$sidAuthUsers    = New-Object System.Security.Principal.SecurityIdentifier 'S-1-5-11'
$sidDomainAdmins = & $sidOf 512
$sidDomainUsers  = & $sidOf 513
$uncRoot = "\\{0}\{1}" -f $env:COMPUTERNAME.ToLower(), $ShareName

Write-Host "Domain controller: $dc"

# --- Share --------------------------------------------------------------------

function New-Rule($sid, $rights, $inherit, $propagate) {
    New-Object System.Security.AccessControl.FileSystemAccessRule ($sid,
        [System.Security.AccessControl.FileSystemRights]$rights,
        [System.Security.AccessControl.InheritanceFlags]$inherit,
        [System.Security.AccessControl.PropagationFlags]$propagate, 'Allow')
}

function Set-ExactAcl($path, $rules) {
    $acl = New-Object System.Security.AccessControl.DirectorySecurity
    $acl.SetAccessRuleProtection($true, $false)
    $acl.SetOwner($sidAdmins)
    foreach ($r in $rules) { $acl.AddAccessRule($r) }
    Set-Acl -LiteralPath $path -AclObject $acl
}

$reportsDir = Join-Path $ShareRoot 'reports'
$null = New-Item -ItemType Directory -Force -Path $reportsDir

$adminRules = @(
    (New-Rule $sidSystem       'FullControl' 'ContainerInherit,ObjectInherit' 'None'),
    (New-Rule $sidAdmins       'FullControl' 'ContainerInherit,ObjectInherit' 'None'),
    (New-Rule $sidDomainAdmins 'FullControl' 'ContainerInherit,ObjectInherit' 'None')
)
Set-ExactAcl $ShareRoot ($adminRules + @(
    (New-Rule $sidDomainUsers 'ReadAndExecute' 'ContainerInherit,ObjectInherit' 'None')
))
Set-ExactAcl $reportsDir ($adminRules + @(
    # Drop box: create files in this folder, but no listing or reading.
    (New-Rule $sidDomainUsers  'CreateFiles,ReadAttributes,Synchronize' 'None' 'None'),
    # Lets hwreport check whether a file name is taken before choosing "-1".
    (New-Rule $sidDomainUsers  'ReadAttributes,Synchronize' 'ObjectInherit' 'InheritOnly'),
    # The user who created a report may write it.
    (New-Rule $sidCreatorOwner 'Write,ReadAttributes,Synchronize' 'ObjectInherit' 'InheritOnly')
))

if (-not (Get-SmbShare -Name $ShareName -ErrorAction SilentlyContinue)) {
    $authUsers   = $sidAuthUsers.Translate([System.Security.Principal.NTAccount]).Value
    $domainAdmin = $sidDomainAdmins.Translate([System.Security.Principal.NTAccount]).Value
    New-SmbShare -Name $ShareName -Path $ShareRoot -ChangeAccess $authUsers -FullAccess $domainAdmin `
        -Description 'hwreport inventory drop folder' | Out-Null
    Write-Host "Created share $uncRoot"
} else {
    Write-Host "Share $uncRoot already exists"
}

# --- Workstations OU ------------------------------------------------------------

$ouDn = "OU=Workstations,$($domain.DistinguishedName)"
if (-not (Get-ADOrganizationalUnit -Filter "DistinguishedName -eq '$ouDn'" -Server $dc)) {
    New-ADOrganizationalUnit -Name 'Workstations' -Path $domain.DistinguishedName `
        -Description 'Active Windows workstations' -ProtectedFromAccidentalDeletion $true -Server $dc
    Write-Host "Created $ouDn"
} else {
    Write-Host "$ouDn already exists"
}

# --- GPO helpers -------------------------------------------------------------------

function New-EmptyGpo($name, $comment) {
    if (Get-GPO -Name $name -Server $dc -ErrorAction SilentlyContinue) {
        throw "GPO '$name' already exists. Delete it or pick another name."
    }
    $gpo = New-GPO -Name $name -Comment $comment -Server $dc
    $gpo.GpoStatus = 'UserSettingsDisabled'
    $gpo
}

function Get-GpoSysvolPath($gpo) {
    Join-Path $env:SystemRoot ("SYSVOL\domain\Policies\{{{0}}}" -f $gpo.Id)
}

# Writes a Group Policy Preferences XML file, registers its client-side
# extension and bumps the computer version so clients reprocess the GPO.
function Add-GppFile($gpo, $relativePath, $xml, $extensionPairs) {
    $file = Join-Path (Get-GpoSysvolPath $gpo) "Machine\Preferences\$relativePath"
    $null = New-Item -ItemType Directory -Force -Path (Split-Path -Parent $file)
    [System.IO.File]::WriteAllText($file, $xml, (New-Object System.Text.UTF8Encoding $true))

    $adObj = Get-ADObject -Identity $gpo.Path -Properties gPCMachineExtensionNames, versionNumber -Server $dc

    # Merge CSE/tool GUID pairs into the sorted "[{cse}{tool}...]" string.
    $map = @{}
    foreach ($m in [regex]::Matches([string]$adObj.gPCMachineExtensionNames, '\[([^\]]+)\]')) {
        $guids = [regex]::Matches($m.Groups[1].Value, '\{[^}]+\}') | ForEach-Object { $_.Value.ToUpper() }
        $map[$guids[0]] = @($guids | Select-Object -Skip 1)
    }
    foreach ($pair in $extensionPairs) {
        $cse = $pair[0].ToUpper(); $tool = $pair[1].ToUpper()
        if (-not $map.ContainsKey($cse)) { $map[$cse] = @() }
        if ($map[$cse] -notcontains $tool) { $map[$cse] = @($map[$cse] + $tool) }
    }
    $ext = ($map.Keys | Sort-Object | ForEach-Object {
        '[' + $_ + (($map[$_] | Sort-Object) -join '') + ']'
    }) -join ''

    $version = [int]$adObj.versionNumber + 1   # computer version = low 16 bits
    Set-ADObject -Identity $gpo.Path -Replace @{ gPCMachineExtensionNames = $ext; versionNumber = $version } -Server $dc

    $gptIni = Join-Path (Get-GpoSysvolPath $gpo) 'GPT.INI'
    $ini = if (Test-Path $gptIni) { [System.IO.File]::ReadAllText($gptIni) } else { "[General]`r`n" }
    if ($ini -match 'Version=\d+') { $ini = $ini -replace 'Version=\d+', "Version=$version" }
    else { $ini = $ini.TrimEnd() + "`r`nVersion=$version`r`n" }
    [System.IO.File]::WriteAllText($gptIni, $ini, [System.Text.Encoding]::ASCII)
}

$cseNull      = '{00000000-0000-0000-0000-000000000000}'
$cseServices  = '{91FBB303-0CD5-4055-BF42-E512A681B325}'; $toolServices = '{CC5746A9-9B74-4BE5-AE2E-64379C86E0E4}'
$cseTasks     = '{AADCED64-746C-4633-A97C-D61349046527}'; $toolTasks    = '{CAB54552-DEEA-4691-817E-ED4A4D1AFC72}'
$changed = (Get-Date).ToString('yyyy-MM-dd HH:mm:ss')
$author  = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name

# --- GPO: WinRM - LAN admins ---------------------------------------------------------

$winrm = New-EmptyGpo $WinRMGpoName "Enables WinRM (HTTP 5985) reachable only from $Subnet on the domain network. Created by deploy\Install-HwreportGpos.ps1."
$svcKey = 'HKLM\Software\Policies\Microsoft\Windows\WinRM\Service'
Set-GPRegistryValue -Guid $winrm.Id -Server $dc -Key $svcKey -ValueName 'AllowAutoConfig' -Type DWord  -Value 1 | Out-Null
Set-GPRegistryValue -Guid $winrm.Id -Server $dc -Key $svcKey -ValueName 'IPv4Filter'      -Type String -Value '*' | Out-Null
Set-GPRegistryValue -Guid $winrm.Id -Server $dc -Key $svcKey -ValueName 'IPv6Filter'      -Type String -Value '*' | Out-Null
$rule = "v2.10|Action=Allow|Active=TRUE|Dir=In|Protocol=6|Profile=Domain|LPort=5985|RA4=$Subnet|Name=WinRM HTTP-In (LAN only, GPO)|Desc=Windows Remote Management from the office LAN only.|"
Set-GPRegistryValue -Guid $winrm.Id -Server $dc -Key 'HKLM\Software\Policies\Microsoft\WindowsFirewall\FirewallRules' `
    -ValueName 'WinRM-HTTP-In-LAN' -Type String -Value $rule | Out-Null

$servicesXml = @"
<?xml version="1.0" encoding="utf-8"?>
<NTServices clsid="{2CFB484A-4E96-4b5d-A0B6-093D2F91E6AE}"><NTService clsid="{AB6F0B67-341F-4e51-92F9-005FBFBA1A43}" name="WinRM" image="2" changed="$changed" uid="{$([guid]::NewGuid().ToString().ToUpper())}"><Properties startupType="AUTOMATIC" serviceName="WinRM" serviceAction="START" timeout="30"/></NTService>
</NTServices>
"@
Add-GppFile $winrm 'Services\Services.xml' $servicesXml @(@($cseNull, $toolServices), @($cseServices, $toolServices))
Write-Host "Created GPO '$WinRMGpoName' ($($winrm.Id))"

# --- GPO: hwreport inventory ------------------------------------------------------

$inventory = New-EmptyGpo $InventoryGpoName "One report per PC: runs $uncRoot\hwreportw.exe as the signed-in user (not SYSTEM). Unlink to remove the task. Created by deploy\Install-HwreportGpos.ps1."
$exe  = "$uncRoot\hwreportw.exe"
$taskArgs = "--out $uncRoot\reports --once $OnceKey"
$today = Get-Date
$start = (New-Object DateTime ($today.Year, $today.Month, 1)).AddMonths(1).ToString('yyyy-MM-dd') + 'T11:00:00'
$taskXml = @"
<?xml version="1.0" encoding="utf-8"?>
<ScheduledTasks clsid="{CC63F200-7309-4ba0-B154-A71CD118DBCC}"><TaskV2 clsid="{D8896631-B747-47a7-84A6-C155337F3BC8}" name="hwreport inventory" image="1" changed="$changed" uid="{$([guid]::NewGuid().ToString().ToUpper())}" removePolicy="1" userContext="0"><Properties action="R" name="hwreport inventory" runAs="BUILTIN\Users" logonType="Group"><Task version="1.2"><RegistrationInfo><Author>$author</Author><Description>Hardware inventory: writes one hwreport JSON per PC to $uncRoot\reports. Managed by the GPO "$InventoryGpoName".</Description></RegistrationInfo><Principals><Principal id="Author"><GroupId>BUILTIN\Users</GroupId><RunLevel>LeastPrivilege</RunLevel></Principal></Principals><Settings><IdleSettings><Duration>PT10M</Duration><WaitTimeout>PT1H</WaitTimeout><StopOnIdleEnd>false</StopOnIdleEnd><RestartOnIdle>false</RestartOnIdle></IdleSettings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><AllowHardTerminate>true</AllowHardTerminate><StartWhenAvailable>true</StartWhenAvailable><RunOnlyIfNetworkAvailable>true</RunOnlyIfNetworkAvailable><AllowStartOnDemand>true</AllowStartOnDemand><Enabled>true</Enabled><Hidden>false</Hidden><RunOnlyIfIdle>false</RunOnlyIfIdle><WakeToRun>false</WakeToRun><ExecutionTimeLimit>PT10M</ExecutionTimeLimit><Priority>7</Priority></Settings><Triggers><LogonTrigger><Enabled>true</Enabled><Delay>PT5M</Delay></LogonTrigger><CalendarTrigger><StartBoundary>$start</StartBoundary><Enabled>true</Enabled><ScheduleByMonth><DaysOfMonth><Day>1</Day></DaysOfMonth><Months><January/><February/><March/><April/><May/><June/><July/><August/><September/><October/><November/><December/></Months></ScheduleByMonth></CalendarTrigger></Triggers><Actions Context="Author"><Exec><Command>$exe</Command><Arguments>$taskArgs</Arguments></Exec></Actions></Task></Properties></TaskV2>
</ScheduledTasks>
"@
Add-GppFile $inventory 'ScheduledTasks\ScheduledTasks.xml' $taskXml @(@($cseNull, $toolTasks), @($cseTasks, $toolTasks))
Write-Host "Created GPO '$InventoryGpoName' ($($inventory.Id))"

Write-Host ""
Write-Host "Next: copy hwreportw.exe (and hwreport.exe, hwoverview.exe) to $ShareRoot, then link the GPOs."
