#!/bin/sh

# Cleanup
/usr/bin/nsenter -m/proc/1/ns/mnt -- fusermount -u /var/lib/lxc/lxcfs 2> /dev/null || true
/usr/bin/nsenter -m/proc/1/ns/mnt -- [ -L /etc/mtab ] || \
        sed -i "/^lxcfs \/var\/lib\/lxc\/lxcfs fuse.lxcfs/d" /etc/mtab

# Prepare
mkdir -p /usr/local/lib/lxcfs /var/lib/lxc/lxcfs

# Update lxcfs
cp -f /lxcfs/lxcfs /usr/local/bin/lxcfs
cp -f /lxcfs/liblxcfs.so /usr/local/lib/lxcfs/liblxcfs.so


# Mount
exec /usr/bin/nsenter -m/proc/1/ns/mnt /usr/local/bin/lxcfs /var/lib/lxc/lxcfs/ &

/bin/debug-agent
