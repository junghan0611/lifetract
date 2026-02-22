{
  description = "lifetract - Life tracking CLI for AI agents (Samsung Health + aTimeLogger)";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in {
        packages.default = pkgs.buildGoModule {
          pname = "lifetract";
          version = "0.1.0";
          src = ./lifetract;
          vendorHash = null; # stdlib only (Phase 1)

          meta = with pkgs.lib; {
            description = "Life tracking CLI for AI agents";
            homepage = "https://github.com/junghan0611/lifetract";
            license = licenses.asl20;
            mainProgram = "lifetract";
          };
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
          ];
        };
      }
    );
}
