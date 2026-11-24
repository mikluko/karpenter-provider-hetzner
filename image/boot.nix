# GRUB on every architecture, installed to the removable path of the ESP and,
# on x86, to the BIOS boot partition as well. A snapshot carries the disk but
# not the builder's firmware variables, so nothing may depend on a boot entry;
# this overrides srvos's Arm profile, which installs systemd-boot and one.
{ lib, pkgs, ... }:
{
  boot.loader = {
    systemd-boot.enable = lib.mkForce false;
    efi.canTouchEfiVariables = lib.mkForce false;
    grub = {
      enable = true;
      efiSupport = true;
      efiInstallAsRemovable = true;
      # On x86, disko adds the disk for the BIOS install from its EF02 partition.
      devices = lib.mkIf (!pkgs.stdenv.hostPlatform.isx86) (lib.mkForce [ "nodev" ]);
    };
  };
}
