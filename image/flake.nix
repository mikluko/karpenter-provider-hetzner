{
  description = "NixOS k0s worker images for karpenter-provider-hetzner";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";
    disko = {
      url = "github:nix-community/disko";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    srvos = {
      url = "github:nix-community/srvos";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    k0s-nix = {
      url = "github:nix-community/k0s-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    nixos-anywhere = {
      url = "github:nix-community/nixos-anywhere";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.disko.follows = "disko";
    };
  };

  outputs =
    {
      nixpkgs,
      disko,
      srvos,
      k0s-nix,
      nixos-anywhere,
      ...
    }:
    let
      inherit (nixpkgs) lib;

      # Hetzner's architecture spelling, as hcloud and imageSelector use it.
      arches = {
        x86 = {
          system = "x86_64-linux";
          modules = [
            srvos.nixosModules.hardware-hetzner-cloud
            ./disk-bios.nix
          ];
        };
        arm = {
          system = "aarch64-linux";
          modules = [
            srvos.nixosModules.hardware-hetzner-cloud-arm
            ./disk-efi.nix
          ];
        };
      };

      # The k0s-nix overlay's k0s_<minor> attributes.
      k0sMinors = [
        "1_34"
        "1_35"
        "1_36"
        "1_37"
      ];

      worker =
        arch: minor:
        lib.nixosSystem {
          inherit (arches.${arch}) system;
          modules = arches.${arch}.modules ++ [
            disko.nixosModules.disko
            k0s-nix.nixosModules.default
            ./worker.nix
            (
              { pkgs, ... }:
              {
                nixpkgs.overlays = [ k0s-nix.overlays.default ];
                services.k0s.package = pkgs."k0s_${minor}";
              }
            )
          ];
        };

      hosts = [
        "aarch64-darwin"
        "x86_64-darwin"
        "aarch64-linux"
        "x86_64-linux"
      ];
    in
    {
      # One worker per architecture and k0s minor, named k0s_<minor>-<arch>,
      # for example k0s_1_36-x86.
      nixosConfigurations = lib.listToAttrs (
        lib.concatMap (
          arch:
          map (minor: {
            name = "k0s_${minor}-${arch}";
            value = worker arch minor;
          }) k0sMinors
        ) (lib.attrNames arches)
      );

      packages = lib.genAttrs hosts (system: {
        nixos-anywhere = nixos-anywhere.packages.${system}.default;
      });

      formatter = lib.genAttrs hosts (system: nixpkgs.legacyPackages.${system}.nixfmt-tree);
    };
}
