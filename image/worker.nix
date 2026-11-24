# A k0s worker that waits for a join. The join writes the token to
# /etc/k0s/join-token and REGISTER_TAINTS and K0S_KUBELET_ARGS to
# /etc/karpenter-hetzner/k0s.env, then starts k0s.service: until the token
# exists, systemd skips the unit at boot.
{ pkgs, ... }:
{
  services.k0s = {
    enable = true;
    role = "worker";
    tokenFile = "/etc/k0s/join-token";
    extraArgs = ''--taints=''${REGISTER_TAINTS} "--kubelet-extra-args=''${K0S_KUBELET_ARGS}"'';
  };
  systemd.services.k0s.serviceConfig.EnvironmentFile = "/etc/karpenter-hetzner/k0s.env";

  # k0s-nix renders the cluster config for every role, and rendering it
  # requires spec.api.address; a worker never reads it.
  environment.etc."k0s/k0s.yaml".enable = false;

  # Hetzner Cloud Firewalls, through the node class's firewallIDs, filter
  # traffic; a host firewall would drop the kubelet's and the CNI's.
  networking.firewall.enable = false;

  services.openssh = {
    enable = true;
    settings.PasswordAuthentication = false;
  };

  # sshd-keygen generates the host keys. cloud-init's ssh module would write
  # the same files, and sshd-keygen fails when both run.
  services.cloud-init.settings.ssh_genkeytypes = [ ];

  fileSystems."/".autoResize = true;

  environment.systemPackages = [
    pkgs.curl
    pkgs.cloud-init
  ];

  documentation.enable = false;

  system.stateVersion = "26.05";
}
