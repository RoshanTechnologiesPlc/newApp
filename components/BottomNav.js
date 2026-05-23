'use client';
import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { Home, Trophy, Newspaper } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import '../lib/i18n';

export default function BottomNav() {
  const pathname = usePathname();
  const { t } = useTranslation();

  const navItems = [
    { name: t('common.matches'), href: '/', icon: Home },
    { name: t('common.leagues'), href: '/leagues', icon: Trophy },
    { name: t('common.news'), href: '/news', icon: Newspaper },
  ];

  return (
    <nav className="fixed bottom-0 left-0 right-0 bg-white border-t border-gray-200 px-6 py-2 flex justify-around items-center">
      {navItems.map((item) => {
        const Icon = item.icon;
        const isActive = pathname === item.href;
        return (
          <Link key={item.name} href={item.href} className={`flex flex-col items-center ${isActive ? 'text-green-600' : 'text-gray-500'}`}>
            <Icon size={24} />
            <span className="text-xs mt-1">{item.name}</span>
          </Link>
        );
      })}
    </nav>
  );
}
