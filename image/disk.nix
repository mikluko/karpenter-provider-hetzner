# Hetzner Cloud boots Arm servers, and some x86 server types, with UEFI, and
# the other x86 types with BIOS. Every image carries an ESP; an x86 image also
# carries a BIOS boot partition, so one snapshot boots on either firmware.
{ lib, pkgs, ... }:
{
  disko.devices.disk.main = {
    type = "disk";
    device = "/dev/sda";
    content = {
      type = "gpt";
      partitions =
        lib.optionalAttrs pkgs.stdenv.hostPlatform.isx86 {
          bios = {
            size = "1M";
            type = "EF02";
          };
        }
        // {
          esp = {
            size = "512M";
            type = "EF00";
            content = {
              type = "filesystem";
              format = "vfat";
              mountpoint = "/boot";
              mountOptions = [ "umask=0077" ];
            };
          };
          root = {
            size = "100%";
            content = {
              type = "filesystem";
              format = "ext4";
              mountpoint = "/";
            };
          };
        };
    };
  };
}
