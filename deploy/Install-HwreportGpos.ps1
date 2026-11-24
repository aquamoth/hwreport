#Requires -Version 3.0
<#
.SYNOPSIS
Creates the hwreport share, the Workstations OU and the unlinked GPO
"WinRM - LAN admins" that collect-reports.ps1 needs.

.DESCRIPTION
Run on a domain controller as a Domain Admin (Windows PowerShell 3.0+, with
the ActiveDirectory and GroupPolicy modules). All AD and SYSVOL writes go to
the DC the script runs on; normal replication carries them to the other DCs.

The script never links a GPO. Link them yourself, for example:
  New-GPLink -Name 'WinRM - LAN admins' -Target 'OU=Workstations,DC=CosmoJPN,DC=local'

Share  (\\<this DC>\<ShareName>, folder <ShareRoot>, with reports\ below it)
  Administrators only. collect-reports.ps1 runs as an admin, copies
  hwreportw.exe to each PC over WinRM and saves the reports itself, so users
  never need access.

GPO "WinRM - LAN admins"
  - Allow remote server management through WinRM (listen on all addresses)
  - Firewall: TCP 5985 inbound, Domain profile, remote addresses <Subnet> only
  - WinRM service: Automatic and started (Group Policy Preferences)
  Over the network Windows filters local accounts out of the Administrators
  token, so in practice only domain accounts that are local admins get in.

Running the script again leaves the OU alone, resets the folder permissions,
and refuses to overwrite an existing GPO with the same name.
#>
[CmdletBinding()]
param(
    [string]$ShareRoot = 'E:\Shares\hwreport',
    [string]$ShareName = 'hwreport$',
    [string]$Subnet    = '192.168.100.0/255.255.255.0',
    [string]$WinRMGpoName = 'WinRM - LAN admins'
)

$ErrorActionPreference = 'Stop'
Import-Module ActiveDirectory, GroupPolicy

$dc      = [System.Net.Dns]::GetHostEntry($env:COMPUTERNAME).HostName
$domain  = Get-ADDomain -Server $dc
$sidOf   = { param($rid) New-Object System.Security.Principal.SecurityIdentifier ("{0}-{1}" -f $domain.DomainSID.Value, $rid) }
$sidSystem       = New-Object System.Security.Principal.SecurityIdentifier 'S-1-5-18'
$sidAdmins       = New-Object System.Security.Principal.SecurityIdentifier 'S-1-5-32-544'
$sidDomainAdmins = & $sidOf 512
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
Set-ExactAcl $ShareRoot $adminRules
Set-ExactAcl $reportsDir $adminRules

if (-not (Get-SmbShare -Name $ShareName -ErrorAction SilentlyContinue)) {
    $domainAdmin = $sidDomainAdmins.Translate([System.Security.Principal.NTAccount]).Value
    New-SmbShare -Name $ShareName -Path $ShareRoot -FullAccess $domainAdmin `
        -Description 'hwreport inventory' | Out-Null
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
$changed = (Get-Date).ToString('yyyy-MM-dd HH:mm:ss')

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


Write-Host ""
Write-Host "Next: copy hwreportw.exe, hwoverview.exe and collect-reports.ps1 to $ShareRoot, then link the GPO to the Workstations OU."
