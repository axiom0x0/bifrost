═══════════════════════════════════════════════════════
  BIFROST MANUAL VALIDATION CHECKLIST
═══════════════════════════════════════════════════════

For each test, scan the QR with your phone. Press Ctrl+C to stop between tests.

──── 1. SEND MODE ────────────────────────────────────
  bifrost ~/seattle-to-sioux-city.gpx

  ✓ QR code displays in terminal
  ✓ Phone opens page with "Download" button + upload form
  ✓ Download works, progress bar shows percentage
  ✓ Upload a file from phone, confirm it lands in current dir

──── 2. SEND WITH CUSTOM PORT ────────────────────────
  bifrost -f ~/seattle-to-sioux-city.gpx -p 9090

  ✓ URL shows port 9090
  ✓ Download still works

──── 3. RECEIVE MODE ─────────────────────────────────
  bifrost -r -o /tmp/bifrost-test

  ✓ Page shows upload form only (no download section)
  ✓ Upload a file from phone
  ✓ Confirm: ls /tmp/bifrost-test/

──── 4. BROWSE MODE ──────────────────────────────────
  bifrost -d ~/Pictures

  ✓ File listing with icons and sizes
  ✓ Tap a file to download it
  ✓ Upload a file, page auto-refreshes showing new file
  ✓ Hidden files (dot-prefix) NOT shown

──── 5. ENCRYPTED SEND ───────────────────────────────
  bifrost -e ~/seattle-to-sioux-city.gpx

  ✓ Banner shows "send 🔒"
  ✓ Page shows "end-to-end encrypted" badge
  ✓ Download works (browser decrypts client-side)
  ✓ Upload works (browser encrypts before sending)

──── 6. ENCRYPTED RECEIVE ────────────────────────────
  bifrost -e -r -o /tmp/bifrost-enc

  ✓ Upload from phone, file is decrypted on save
  ✓ Confirm content: cat /tmp/bifrost-enc/<yourfile>

──── 7. ENCRYPTED BROWSE ─────────────────────────────
  bifrost -e -d ~/Documents

  ✓ Download decrypts correctly
  ✓ Upload encrypts in browser, server decrypts on save

──── 8. ONE-SHOT MODE ────────────────────────────────
  bifrost -1 ~/seattle-to-sioux-city.gpx

  ✓ Download the file from phone
  ✓ Server exits automatically after transfer

──── 9. ONE-SHOT RECEIVE ─────────────────────────────
  bifrost -1 -r -o /tmp/bifrost-oneshot

  ✓ Upload one file from phone
  ✓ Server exits automatically after upload

──── 10. ERROR HANDLING ──────────────────────────────
  bifrost ~/seattle-to-sioux-city.gpx
  Then visit http://<ip>:8888/fakepath in your phone browser

  ✓ Styled 404 page (dark theme, "Not Found", back link)
  ✓ NOT a raw text error

──── 11. VERSION ─────────────────────────────────────
  bifrost -v

  ✓ Prints version string

──── 12. HELP ────────────────────────────────────────
  bifrost -h

  ✓ Shows version in header
  ✓ Lists all flags including -1

──── CLEANUP ─────────────────────────────────────────
  rm -rf /tmp/bifrost-test /tmp/bifrost-enc /tmp/bifrost-oneshot

═══════════════════════════════════════════════════════
