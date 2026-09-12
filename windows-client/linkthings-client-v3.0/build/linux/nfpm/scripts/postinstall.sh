#!/bin/sh

# Grant the binary the one Linux capability its tunnel/adapter/routing code
# needs (CAP_NET_ADMIN) so installed users never see the app's own one-time
# pkexec prompt - see linkthings-client-v3.0/CLAUDE.md's operational caveat
# for why this is a capability grant, not a setuid/root bit, and why it does
# not also cover the separate polkit prompt for DNS configuration.
if command -v setcap >/dev/null 2>&1; then
  echo "Granting CAP_NET_ADMIN to /usr/local/bin/linkthings-client..."
  setcap cap_net_admin+ep /usr/local/bin/linkthings-client || \
    echo "Warning: setcap failed. The app will prompt via pkexec on first launch instead." >&2
else
  echo "Warning: setcap not found (install libcap2-bin/libcap-utils). The app will prompt via pkexec on first launch instead." >&2
fi

# Update desktop database for .desktop file changes
# This makes the application appear in application menus and registers its capabilities.
if command -v update-desktop-database >/dev/null 2>&1; then
  echo "Updating desktop database..."
  update-desktop-database -q /usr/share/applications
else
  echo "Warning: update-desktop-database command not found. Desktop file may not be immediately recognized." >&2
fi

# Update MIME database for custom URL schemes (x-scheme-handler)
# This ensures the system knows how to handle your custom protocols.
if command -v update-mime-database >/dev/null 2>&1; then
  echo "Updating MIME database..."
  update-mime-database -n /usr/share/mime
else
  echo "Warning: update-mime-database command not found. Custom URL schemes may not be immediately recognized." >&2
fi

exit 0
