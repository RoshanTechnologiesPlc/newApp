'use client';
import { mockStandings } from '../../lib/mock-data';
import StandingsTable from '../../components/StandingsTable';
import { useTranslation } from 'react-i18next';
import '../../lib/i18n';

export default function LeaguesPage() {
  const { t } = useTranslation();

  return (
    <div>
      <div className="bg-gray-100 p-3 text-sm font-semibold text-gray-600">
        የኢትዮጵያ ፕሪምየር ሊግ - {t('common.standings')}
      </div>
      <StandingsTable standings={mockStandings} />
    </div>
  );
}
