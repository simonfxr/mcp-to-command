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
          mcp-to-command = pkgs.buildGoModule {
            inherit pname;
            version = "0.0.0" + (if self ? shortRev then "+${self.shortRev}" else "");
            inherit src;

            subPackages = [ "." ];

            vendorHash = "sha256-nS4VyQLoQ7QU+8sYolBVgKHVNSmyhXwkzdUO7yHjsCg=";

            ldflags = [
              "-s"
              "-w"
            ];

            meta.mainProgram = pname;
          };

          default = mcp-to-command;
        };

        apps.default = flake-utils.lib.mkApp { drv = packages.default; };

        checks.default = packages.default;

        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
            gotools
            go-tools
            delve
          ];
        };

        formatter = pkgs.nixfmt-rfc-style;
      }
    );
}
