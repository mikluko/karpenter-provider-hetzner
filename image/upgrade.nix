{
  config,
  lib,
  ...
}:
let
  cfg = config.karpenter.nodeUpgrade;
in
{
  options.karpenter.nodeUpgrade.flake = lib.mkOption {
    type = lib.types.nullOr lib.types.str;
    default = null;
    example = "git+https://example.com/nodes.git#k0s_1_36-x86";
    description = ''
      Flake reference, with its nixosConfiguration attribute, that a node
      rebuilds itself from on its first boot, before cloud-init runs the join.
      A failed rebuild leaves the node as its image built it. Null disables
      the rebuild.
    '';
  };

  config = lib.mkIf (cfg.flake != null) {
    nix.settings.experimental-features = [
      "nix-command"
      "flakes"
    ];

    # The new system takes effect at a reboot rather than a switch: switching
    # inside the boot transaction can wait on the very jobs it is part of.
    # After `systemctl reboot` the unit never completes, so cloud-final, which
    # runs the join, does not start on the old system; it runs after the
    # reboot, since cloud-init has not yet marked its scripts done.
    systemd.services.node-upgrade = {
      description = "Rebuild the node from its flake before the join";
      wantedBy = [ "multi-user.target" ];
      wants = [ "network-online.target" ];
      after = [
        "network-online.target"
        "cloud-init.service"
      ];
      before = [ "cloud-final.service" ];
      unitConfig.ConditionFirstBoot = true;
      path = [
        config.nix.package
        config.system.build.nixos-rebuild
      ];
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
        TimeoutStartSec = "20min";
      };
      script = ''
        nixos-rebuild boot --flake ${lib.escapeShellArg cfg.flake}
        if [ "$(readlink -f /nix/var/nix/profiles/system)" != "$(readlink -f /run/current-system)" ]; then
          systemctl reboot --no-block
          exec sleep infinity
        fi
      '';
    };
  };
}
