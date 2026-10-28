{
  description = "mcp-to-command";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
        pname = "mcp-to-command";
        version = "0.1.0" + (if self ? shortRev then "+${self.shortRev}" else "");
        fs = pkgs.lib.fileset;
        src = fs.toSource {
          root = ./.;
          fileset = fs.unions [
            (fs.fileFilter (file: file.hasExt "go") ./.)
            (fs.fileFilter (file: file.name == "go.mod") ./.)
            (fs.fileFilter (file: file.name == "go.sum") ./.)
          ];
        };
      in
      rec {
        packages = rec {
          mcp-to-command = pkgs.buildGo127Module {
            inherit pname version src;

            subPackages = [ "." ];

            vendorHash = "sha256-36nWZqUptizcLTGddXwf81V0tJ7uXs1ky2+sM+FVH98=";

            ldflags = [
              "-s"
              "-w"
              "-X main.version=${version}"
            ];

            meta.mainProgram = pname;
          };

          default = mcp-to-command;
        };

        apps.default = flake-utils.lib.mkApp { drv = packages.default; };

        checks.default = packages.default;

        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            go_1_27
            gopls
            gotools
            (go-tools.override { buildGoModule = buildGo127Module; })
            delve
          ];
        };

        formatter = pkgs.nixfmt;
      }
    );
}
