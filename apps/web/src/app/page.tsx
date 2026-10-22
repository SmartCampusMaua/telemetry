import Link from "next/link";

export default function Home() {
  return (
    <div className="flex flex-col">
      <Link href="/motor">Motor</Link>
      <Link href="/eletrica">Elétrica</Link>
      <Link href="/dynamics">Dynamics</Link>
      <Link href="/gps">GPS</Link>
    </div>
  );
}
