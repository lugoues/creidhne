package main

import "testing"

func TestParseManXML(t *testing.T) {
	// A shared entry (Before=/After=) with a citerefentry and a custom entity.
	sample := []byte(`<refentry><refsect1><variablelist>
  <varlistentry>
    <term><varname>Before=</varname></term>
    <term><varname>After=</varname></term>
    <listitem>
      <para>These two settings expect a space-separated list of unit names.</para>
      <para>Configure ordering dependencies, see
        <citerefentry><refentrytitle>systemd.service</refentrytitle><manvolnum>5</manvolnum></citerefentry>
        and version &version;.</para>
      <para>Added in version 201.</para>
    </listitem>
  </varlistentry>
  <varlistentry>
    <term><varname>Description=</varname></term>
    <listitem><para>A human readable name. &SOME_PATH; is referenced.</para></listitem>
  </varlistentry>
</variablelist></refsect1></refentry>`)

	docs := parseManXML(sample, "systemd.unit", "257")

	// Before=/After= share an entry: same header, anchor is the first varname.
	for _, name := range []string{"Before", "After"} {
		e, ok := docs[name]
		if !ok {
			t.Fatalf("%s not parsed", name)
		}
		if e.anchor != "Before=" {
			t.Errorf("%s anchor = %q, want Before=", name, e.anchor)
		}
		if len(e.paras) != 3 {
			t.Fatalf("%s paras = %#v, want 3", name, e.paras)
		}
		// citerefentry -> title(vol); &version; -> 257; whitespace collapsed.
		if want := "Configure ordering dependencies, see systemd.service(5) and version 257."; e.paras[1] != want {
			t.Errorf("para[1] = %q, want %q", e.paras[1], want)
		}
	}

	// A custom entity in prose becomes its bare name (best-effort).
	if d := docs["Description"]; d.paras[0] != "A human readable name. SOME_PATH is referenced." {
		t.Errorf("Description para = %q", d.paras[0])
	}
}
