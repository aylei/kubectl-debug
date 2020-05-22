#!/bin/sh
set -x 

# Cleanup
/usr/bin/nsenter -m/proc/1/ns/mnt -- fusermount -u /var/lib/lxc/lxcfs 2> /dev/null || true
/usr/bin/nsenter -m/proc/1/ns/mnt -- [ -L /etc/mtab ] || \
        sed -i "/^lxcfs \/var\/lib\/lxc\/lxcfs fuse.lxcfs/d" /etc/mtab

# Prepare
/usr/bin/nsenter -m/proc/1/ns/mnt -- mkdir -p /var/lib/lxc/lxcfs

# Mount
LXCFS_USR=/usr/bin/lxcfs
LXCFS=/usr/local/bin/lxcfs
/usr/bin/nsenter -m/proc/1/ns/mnt -- [ -f $LXCFS_USR ] && LXCFS=$LXCFS_USR
exec /usr/bin/nsenter -m/proc/1/ns/mnt -- $LXCFS -p "/run/lxcfs-$$.pid" /var/lib/lxc/lxcfs/ &

if grep -q io.containerd.runtime.v1.linux /proc/$PPID/cmdline 
then
  export KCTLDBG_CONTAINERDV1_SHIM=io.containerd.runc.v1
fi   

/bin/debug-agent "$@"
