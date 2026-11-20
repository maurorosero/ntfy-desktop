# ntfy-desktop — repositorio de paquetes

## apt (Debian, Ubuntu y derivadas)

```sh
sudo install -d -m 0755 /etc/apt/keyrings
curl -fsSL https://maurorosero.github.io/ntfy-desktop/keys/repo-key.asc | sudo tee /etc/apt/keyrings/ntfy-desktop.asc >/dev/null
echo "deb [signed-by=/etc/apt/keyrings/ntfy-desktop.asc] https://maurorosero.github.io/ntfy-desktop/apt stable main" | sudo tee /etc/apt/sources.list.d/ntfy-desktop.list
sudo apt update && sudo apt install ntfy-desktop
```

## rpm (Fedora, openSUSE y derivadas)

```sh
sudo curl -fsSL https://maurorosero.github.io/ntfy-desktop/keys/repo-key.asc -o /etc/pki/rpm-gpg/RPM-GPG-KEY-ntfy-desktop
sudo dnf config-manager --add-repo https://maurorosero.github.io/ntfy-desktop/rpm/<arch>
sudo dnf install ntfy-desktop
```

## Versiones publicadas

- `ntfy-desktop_0.2.0_linux_amd64.deb`
- `ntfy-desktop_0.2.0_linux_arm64.deb`
