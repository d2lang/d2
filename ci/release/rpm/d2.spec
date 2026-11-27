Name:           d2
Version:        %{d2_version}
Release:        %{d2_release}
Summary:        Declarative diagram scripting language
License:        MPL-2.0
URL:            https://d2lang.com/
Source0:        d2
Source1:        d2.1.gz
Source2:        LICENSE.txt
Source3:        THIRD_PARTY_NOTICES.txt
AutoReqProv:    no

# This package redistributes the exact binary from the upstream release.
# Skip build-id links, debuginfo, stripping and other buildroot mutations.
%global _build_id_links none
%global debug_package %{nil}
%global __os_install_post %{nil}
%global _buildhost reproducible
%global use_source_date_epoch_as_buildtime 1
%global clamp_mtime_to_source_date_epoch 1
%global _binary_payload w6.xzdio
%global license_dir %{_datadir}/licenses/%{name}

%description
D2 turns text descriptions into diagrams, including SVG images.

%install
install -Dpm 0755 "%{SOURCE0}" "%{buildroot}%{_bindir}/d2"
install -Dpm 0644 "%{SOURCE1}" "%{buildroot}%{_mandir}/man1/d2.1.gz"
install -Dpm 0644 "%{SOURCE2}" "%{buildroot}%{license_dir}/LICENSE.txt"
install -Dpm 0644 "%{SOURCE3}" "%{buildroot}%{license_dir}/THIRD_PARTY_NOTICES.txt"
touch -r "%{SOURCE0}" "%{buildroot}%{license_dir}"

%files
%defattr(-,root,root,-)
%{_bindir}/d2
%{_mandir}/man1/d2.1.gz
%dir %{license_dir}
%license %{license_dir}/LICENSE.txt
%license %{license_dir}/THIRD_PARTY_NOTICES.txt
