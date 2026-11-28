{
  description = "karpenter-provider-hetzner";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";

  outputs =
    { self, nixpkgs }:
    let
      inherit (nixpkgs) lib;

      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];

      # The image targets, named by GOARCH.
      arches = [
        "amd64"
        "arm64"
      ];

      # The release commit writes VERSION; a pre-release build overwrites it
      # in the checkout and stages it, so the flake reads the uncommitted value.
      version = lib.trim (builtins.readFile ./VERSION);
      commit = self.rev or self.dirtyRev or "unknown";

      forSystems = f: lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

      goFor = pkgs: pkgs.go_1_27;

      src = lib.fileset.toSource {
        root = ./.;
        fileset = lib.fileset.unions [
          ./go.mod
          ./go.sum
          ./.golangci.yml
          (lib.fileset.fileFilter (f: f.hasExt "go") ./.)
          ./hack
          ./charts/karpenter-provider-hetzner/crds
        ];
      };

      controllerFor =
        pkgs: goos: goarch:
        (pkgs.buildGoModule.override { go = goFor pkgs; }) {
          pname = "karpenter-provider-hetzner";
          inherit version src;
          vendorHash = "sha256-VKzhCIyxDs06jNdJ9dsxYDaeXGCv/itT8lVtnIOqT84=";
          subPackages = [ "cmd/controller" ];
          env.CGO_ENABLED = "0";
          ldflags = [
            "-s"
            "-w"
          ];
          # buildGoModule takes GOOS and GOARCH from its go and does not let
          # env override them; a static binary needs no cross stdenv.
          preBuild = ''
            export GOOS=${goos} GOARCH=${goarch}
          '';
          postInstall = ''
            if [ -d $out/bin/${goos}_${goarch} ]; then
              mv $out/bin/${goos}_${goarch}/* $out/bin/
              rmdir $out/bin/${goos}_${goarch}
            fi
            mv $out/bin/controller $out/bin/karpenter-provider-hetzner
          '';
          # The tests run in checks.
          doCheck = false;
          meta.mainProgram = "karpenter-provider-hetzner";
        };

      controller = pkgs: controllerFor pkgs (goFor pkgs).GOOS (goFor pkgs).GOARCH;

      imageFor =
        pkgs: goarch:
        pkgs.dockerTools.streamLayeredImage {
          name = "sarak.sh/mikluko/karpenter-provider-hetzner";
          tag = version;
          architecture = goarch;
          contents = [
            (pkgs.buildEnv {
              name = "karpenter-provider-hetzner-root";
              paths = [
                pkgs.cacert
                (pkgs.dockerTools.fakeNss.override {
                  extraPasswdLines = [ "nonroot:x:65532:65532:nonroot:/home/nonroot:/sbin/nologin" ];
                  extraGroupLines = [ "nonroot:x:65532:" ];
                })
              ];
              pathsToLink = [ "/etc" ];
            })
          ];
          config = {
            Entrypoint = [ (lib.getExe (controllerFor pkgs "linux" goarch)) ];
            User = "65532:65532";
            Labels = {
              "org.opencontainers.image.source" = "https://sarak.sh/karpenter/provider-hetzner";
              "org.opencontainers.image.revision" = commit;
              "org.opencontainers.image.version" = version;
            };
          };
        };

      # A check is the package's own build environment running a command in
      # place of the build: same Go, same sources, same module cache, inside
      # the nix sandbox, which has a loopback of its own and no network.
      checkWith =
        pkgs: name: nativeBuildInputs: command:
        (controller pkgs).overrideAttrs (old: {
          pname = "karpenter-provider-hetzner-${name}";
          nativeBuildInputs = old.nativeBuildInputs ++ nativeBuildInputs;
          buildPhase = ''
            runHook preBuild
            export HOME=$TMPDIR
            ${command}
            runHook postBuild
          '';
          installPhase = "touch $out";
          postInstall = "";
          dontFixup = true;
        });
    in
    {
      packages = forSystems (
        pkgs:
        {
          default = controller pkgs;
        }
        // lib.optionalAttrs pkgs.stdenv.hostPlatform.isLinux (
          lib.listToAttrs (map (goarch: lib.nameValuePair "image-${goarch}" (imageFor pkgs goarch)) arches)
        )
      );

      checks = forSystems (pkgs: {
        # -race needs cgo, which the package itself builds without. The
        # envtest suites skip here: they need KUBEBUILDER_ASSETS.
        test = checkWith pkgs "test" [ ] "CGO_ENABLED=1 go test -race -count=1 ./...";
        lint = checkWith pkgs "lint" [ pkgs.golangci-lint ] "golangci-lint run ./...";
      });

      devShells = forSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [
            (goFor pkgs)
            pkgs.golangci-lint
            pkgs.go-task
            pkgs.crane
            pkgs.jq
            pkgs.kubernetes-helm
          ];
        };
      });

      formatter = forSystems (pkgs: pkgs.nixfmt);
    };
}
