param(
  [string]$BaseUrl = "http://127.0.0.1:8080/api/v1",
  [string]$AdminUsername = "admin",
  [string]$AdminPassword = "admin",
  [string]$CredentialName = "test-root-password",
  [string]$SshUsername = "root",
  [string]$SshPassword = "123"
)

$ErrorActionPreference = "Stop"

function Invoke-PanelApi {
  param(
    [Parameter(Mandatory = $true)][string]$Method,
    [Parameter(Mandatory = $true)][string]$Path,
    [object]$Body = $null
  )

  $uri = "$BaseUrl$Path"
  $params = @{
    Method      = $Method
    Uri         = $uri
    WebSession  = $script:Session
    ContentType = "application/json"
  }
  if ($null -ne $Body) {
    $params.Body = ($Body | ConvertTo-Json -Depth 8)
  }

  $response = Invoke-RestMethod @params
  if ($null -ne $response.error) {
    throw "API error $($response.error.code): $($response.error.message)"
  }
  return $response.data
}

function Upsert-Credential {
  $credentials = Invoke-PanelApi -Method "GET" -Path "/credentials"
  $existing = @($credentials | Where-Object { $_.name -eq $CredentialName -and $_.type -eq "password" -and $_.username -eq $SshUsername } | Select-Object -First 1)
  $body = @{
    name     = $CredentialName
    type     = "password"
    username = $SshUsername
    password = $SshPassword
  }

  if ($existing.Count -gt 0) {
    $credential = Invoke-PanelApi -Method "PUT" -Path "/credentials/$($existing[0].id)" -Body $body
    Write-Host "Updated credential $($credential.name) ($($credential.id))"
    return $credential
  }

  $credential = Invoke-PanelApi -Method "POST" -Path "/credentials" -Body $body
  Write-Host "Created credential $($credential.name) ($($credential.id))"
  return $credential
}

function Upsert-Server {
  param(
    [Parameter(Mandatory = $true)][string]$Name,
    [Parameter(Mandatory = $true)][string]$HostName,
    [Parameter(Mandatory = $true)][string]$Distro,
    [Parameter(Mandatory = $true)][string]$CredentialId
  )

  $servers = Invoke-PanelApi -Method "GET" -Path "/servers"
  $existing = @($servers | Where-Object { $_.host -eq $HostName } | Select-Object -First 1)
  $body = @{
    name         = $Name
    host         = $HostName
    port         = 22
    sshUsername  = $SshUsername
    credentialId = $CredentialId
    labels       = @("test", "debian", "phase2")
    notes        = "Seeded test server from docs/servers.md ($Distro)."
  }

  if ($existing.Count -gt 0) {
    $server = Invoke-PanelApi -Method "PUT" -Path "/servers/$($existing[0].id)" -Body $body
    Write-Host "Updated server $($server.name) $($server.host) ($($server.id))"
    return
  }

  $server = Invoke-PanelApi -Method "POST" -Path "/servers" -Body $body
  Write-Host "Created server $($server.name) $($server.host) ($($server.id))"
}

$script:Session = New-Object Microsoft.PowerShell.Commands.WebRequestSession

Invoke-RestMethod `
  -Method "POST" `
  -Uri "$BaseUrl/auth/login" `
  -WebSession $script:Session `
  -ContentType "application/json" `
  -Body (@{ username = $AdminUsername; password = $AdminPassword } | ConvertTo-Json) | Out-Null

$credential = Upsert-Credential

Upsert-Server -Name "Debian 13 Test" -HostName "192.168.242.130" -Distro "Debian 13" -CredentialId $credential.id
Upsert-Server -Name "Debian 12 Test" -HostName "192.168.242.131" -Distro "Debian 12" -CredentialId $credential.id

Write-Host "Seed complete."
