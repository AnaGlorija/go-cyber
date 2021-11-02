#!/bin/sh
# Installation script for cyber. It tries to move $bin in one of the
# directories stored in $binpaths.

binpaths="/usr/local/bin /usr/bin"
libpaths="/usr/lib /usr/local/lib"


# This variable contains a nonzero length string in case the script fails
# because of missing write permissions.
is_write_perm_missing=""

# Download archive with cyberdcli binaries according to platform type
PLATFORM=$(uname)
case "$PLATFORM" in
#  "Darwin")
#    # macOS
#    curl -OL  https://github.com/cybercongress/go-cyber/releases/download/v0.1.6/cyberdcli_v0.1.6_darwin-amd64.tar.gz
#    tar -xzf cyberdcli_v0.1.6_darwin-amd64.tar.gz
#    for binpath in $binpaths; do
#      if cp build_v0.1.6_darwin_amd64/cyberdcli "$binpath"; then
#        for libpath in $libpaths; do
#          if cp build_v0.1.6_darwin_amd64/libgo_cosmwasm.dylib  "$libpath"; then
#            cp build_v0.1.6_darwin_amd64/libgo_cosmwasm.so  "$libpath"
#            echo "Moved libgo_cosmwasm to $libpath"
#            break
#          else
#          if [ -d "$libpath" ] && [ ! -w "$libpath" ]; then
#            is_write_perm_missing=1
#          fi
#          fi
#        done
#        echo "Moved $bin to $binpath"
#        echo "Enjoy your cyber experience!"
#        rm cyberdcli_v0.1.6_darwin-amd64.tar.gz
#        rm -rf build_v0.1.6_darwin_amd64
#        exit 0
#      else
#      if [ -d "$binpath" ] && [ ! -w "$binpath" ]; then
#        is_write_perm_missing=1
#        rm cyberdcli_v0.1.6_darwin-amd64.tar.gz
#        rm -rf build_v0.1.6_darwin_amd64
#      fi
#      fi
#    done
#    ;;
      "Linux")
    # Linux distro,
    curl -OL https://github.com/cybercongress/go-cyber/releases/download/v0.2.0/cyber_v0.2.0_linux-amd64.tar.gz
    tar -xzf cyber_v0.2.0_linux-amd64.tar.gz -C ./
    for binpath in $binpaths; do
      if cp build_v0.2.0_linux_amd64/cyber "$binpath"; then
        for libpath in $libpaths; do
          if cp build_v0.2.0_linux_amd64/libgo_cosmwasm.dylib  "$libpath"; then
            cp build_v0.2.0_linux_amd64/libgo_cosmwasm.so  "$libpath"
            echo "Moved libgo_cosmwasm to $libpath"
            break
          else
          if [ -d "$libpath" ] && [ ! -w "$libpath" ]; then
            is_write_perm_missing=1
          fi
          fi
        done
        echo "Moved $bin to $binpath"
        echo "Enjoy your cyber experience!"
        rm cyber_v0.2.0_linux-amd64.tar.gz
        rm -rf build_v0.2.0_linux_amd64
        exit 0
      else
      if [ -d "$binpath" ] && [ ! -w "$binpath" ]; then
        is_write_perm_missing=1
        rm cyber_v0.2.0_linux-amd64.tar.gz
        rm -rf build_v0.2.0_linux_amd64
      fi
      fi
    done
    ;;
esac


echo "We cannot install $bin in one of the directories $binpaths"

if [ -n "$is_write_perm_missing" ]; then
  echo "It seems that we do not have the necessary write permissions."
  echo "Perhaps try running this script as a privileged user:"
  echo "Or check that you using default library path."
  echo "    sudo $0"
  echo
fi

exit 1
