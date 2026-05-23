'use client';
import { mockMatches } from '../lib/mock-data';
import MatchCard from '../components/MatchCard';
import { useTranslation } from 'react-i18next';
import '../lib/i18n';

export default function MatchesPage() {
  const { t } = useTranslation();

  return (
    <div>
      <div className="bg-gray-100 p-3 text-sm font-semibold text-gray-600">
        {t('common.today')}
      </div>
      <div className="flex flex-col">
        {mockMatches.map((match) => (
          <MatchCard key={match.id} match={match} />
        ))}
      </div>
    </div>
  );
}
