$ProgressPreference='SilentlyContinue'
Invoke-WebRequest -Uri 'https://get.helm.sh/helm-v3.14.4-windows-amd64.zip' -OutFile "$env:TEMP\helm.zip"
Expand-Archive "$env:TEMP\helm.zip" -DestinationPath "$env:TEMP\helm-bin" -Force
& "$env:TEMP\helm-bin\windows-amd64\helm.exe" version
