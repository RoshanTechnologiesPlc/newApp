'use client';
import { useTranslation } from 'react-i18next';
import '../lib/i18n';

export default function StandingsTable({ standings }) {
  const { t } = useTranslation();

  return (
    <div className="w-full overflow-x-auto">
      <table className="w-full text-sm text-left">
        <thead className="bg-gray-50 text-gray-600">
          <tr>
            <th className="px-4 py-2 font-medium">{t('common.pos')}</th>
            <th className="px-4 py-2 font-medium">{t('common.team')}</th>
            <th className="px-4 py-2 font-medium">{t('common.played')}</th>
            <th className="px-4 py-2 font-medium">{t('common.points')}</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-100">
          {standings.map((row) => (
            <tr key={row.pos} className="bg-white">
              <td className="px-4 py-3">{row.pos}</td>
              <td className="px-4 py-3 font-medium">{row.team}</td>
              <td className="px-4 py-3">{row.played}</td>
              <td className="px-4 py-3 font-bold">{row.points}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
